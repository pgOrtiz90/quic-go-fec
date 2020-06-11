package rdecoder

import (
	"github.com/lucas-clemente/quic-go/rquic"
	"github.com/lucas-clemente/quic-go/rquic/schemes"
	"github.com/lucas-clemente/quic-go/rquic/rLogger"
)

type Decoder struct {
	pktsSrc []*parsedSrc
	pktsCod []*parsedCod

	lenDCID int // length of DCID

	lastScheme uint8
	coeff      schemes.CoeffUnpacker
	didRecover bool

	// Obsolete packets detection
	lastSeenGen        uint8
	lastSeenPkt        uint8
	obsoleteXhold      uint8 // rQUIC ID of the last valid packet
	obsoleteSrcChecked bool

	srcAvbl           []uint8 // used for building srcMiss (missing SRC)
	srcMiss           []uint8 // used for Decoder.Recover()
	doCheckMissingSrc bool    // flag for updating the list of srcMiss

	pollutionCount float64
}

func (d *Decoder) Process(raw []byte, currentSCIDLen int) (uint8, bool) {
	d.lenDCID = currentSCIDLen
	d.didRecover = false
	d.obsoleteSrcChecked = false

	rHdrPos := d.rQuicHdrPos()
	ptype := raw[rHdrPos+rquic.FieldPosTypeScheme]

	logUnknownPkt := func() {
		if rLogger.IsEnabled() {
			rLogger.Printf("Decoder Packet UNKNOWN pkt.Len:%d DCID.Len:%d hdr(hex):[% X]",
				len(raw), d.lenDCID, raw[:rHdrPos+rquic.CodHeaderSizeMax],
			)
		}
	}

	// unprotected packet
	if ptype == 0 {
		if rLogger.IsDebugging() {
			rLogger.Printf("Decoder Packet pkt.Len:%d DCID.Len:%d hdr(hex):[% X]",
				len(raw), d.lenDCID, raw[:rHdrPos+rquic.FieldSizeTypeScheme],
			)
		}
		return rquic.TypeUnprotected, d.didRecover
	}

	// protected packet
	if ptype&rquic.MaskType == 0 {
		if src := d.NewSrc(raw); src != nil {
			d.optimizeWithSrc(src, 0)
			return rquic.TypeProtected, d.didRecover
		}
		logUnknownPkt()
		return rquic.TypeUnknown, d.didRecover
	}

	// coded packet
	if ptype&rquic.MaskType != 0 {
		d.NewCod(raw)
		d.Recover()
		return rquic.TypeCoded, d.didRecover
	}

	// Coded and unknown packets are not passed to QUIC
	logUnknownPkt()
	return rquic.TypeUnknown, d.didRecover
}

func (d *Decoder) NewSrc(raw []byte) *parsedSrc {
	rHdrPos := d.rQuicHdrPos()
	srcPldPos := d.rQuicSrcPldPos()
	if rLogger.IsDebugging() {
		rLogger.Printf("Decoder Packet pkt.Len:%d DCID.Len:%d hdr(hex):[% X]",
			len(raw), d.lenDCID, raw[:rHdrPos+srcPldPos],
		)
	}

	ps := &parsedSrc{
		id:      raw[rHdrPos+rquic.FieldPosId],
		lastGen: raw[rHdrPos+rquic.FieldPosLastGen],
		overlap: raw[rHdrPos+rquic.FieldPosOverlap],
		fwd:     &raw[rHdrPos+rquic.FieldPosTypeScheme], // reusing scheme field
		pld:     raw[srcPldPos:],
	}
	ps.codedLen = rquic.PldLenPrepare(len(ps.pld))

	d.updateScope(ps)
	if d.isObsoletePktAndUpdateXhold(ps) {
		return nil
	} // if SRC is TOO OLD
	if d.srcAvblUpdate(ps.id) {
		return nil
	} // or REPEATED, discard it

	*ps.fwd = rquic.FlagSource
	raw[rHdrPos+rquic.FieldPosGenSize] = 0 // rQUIC field reused for showing number of coefficients in the header
	d.pktsSrc = append(d.pktsSrc, ps)

	rLogger.MaybeIncreaseRxSrc()

	return ps
}

func (d *Decoder) NewSrcRec(cod *parsedCod) *parsedSrc {
	// Recovered too late or already received in a retransmission? Discard.
	if obsolete, duplicate := d.isObsoleteId(cod.srcIds[0]), d.srcAvblUpdate(cod.srcIds[0]); obsolete || duplicate {
		if rLogger.IsDebugging() {
			rLogger.Printf("Decoder Packet Recovered DISCARDED pkt.ID:%d Obsolete:%t Duplicate:%t",
				cod.srcIds[0], obsolete, duplicate,
			)
		}
		cod.markAsObsolete()
		return nil
	}
	// cod.pld must be fully decoded. If not, discard.
	if cod.remaining > 1 {
		if rLogger.IsEnabled() {
			rLogger.Printf("ERROR Decoder RecoveredPkt NotDecoded srcIDs:%d coeffs:%d", cod.srcIds, cod.coeff)
		}
		cod.markAsObsolete()
		return nil
	}
	cod.scaleDown()
	*cod.fwd |= rquic.FlagSource

	ps := &parsedSrc{
		id: cod.srcIds[0],
		//// These fields are consulted only when brand new packet is received
		// lastSeenGen: cod.genId
		// overlap: 0
		fwd: cod.fwd,
		pld: cod.pld[rquic.LenOfSrcLen:rquic.PldLenRead(cod.pld, 0)],
	}
	d.updateScope(ps)
	d.pktsSrc = append(d.pktsSrc, ps)
	d.didRecover = true

	rLogger.MaybeIncreaseRxRec()
	if rLogger.IsDebugging() {
		srcPldPos := d.rQuicSrcPldPos()
		rLogger.Printf("Decoder Packet Recovered pkt.Len:%d DCID.Len:%d hdr(hex):[% X]",
			srcPldPos+len(ps.pld), d.lenDCID, ps.pld[:srcPldPos],
		)
	}

	return ps
}

func (d *Decoder) NewCod(raw []byte) {
	rHdrPos := d.rQuicHdrPos()

	rLogger.MaybeIncreaseRxCod()
	if rLogger.IsDebugging() {
		rLogger.Printf("Decoder Packet pkt.Len:%d DCID.Len:%d hdr(hex):[% X]",
			len(raw), d.lenDCID, raw[:rHdrPos+rquic.CodHeaderSizeMax],
		)
	}

	pc := &parsedCod{
		genSize: raw[rHdrPos+rquic.FieldPosGenSize],
		genId:   raw[rHdrPos+rquic.FieldPosGenId],
	}
	// till pc is optimized at the end of this method, remaining == genSize
	pc.remaining = int(pc.genSize)

	// List of SRC IDs covered by this COD
	pc.srcIds = make([]uint8, pc.remaining)
	pc.id = raw[rHdrPos+rquic.FieldPosId]
	pc.srcIds[0] = pc.id - uint8(pc.remaining) + 1
	for i := 1; i < pc.remaining; i++ {
		pc.srcIds[i] = pc.srcIds[i-1] + 1
	}
	d.updateScope(pc)
	if d.isObsoletePktAndUpdateXhold(pc) {
		return
	}

	// Update scheme
	// The use of different schemes at a time is very unlikely.
	newScheme := raw[rHdrPos+rquic.FieldPosTypeScheme] & rquic.MaskScheme
	if d.lastScheme != newScheme {
		d.coeff = schemes.MakeCoeffUnpacker(newScheme)
		d.lastScheme = newScheme
	}
	// Get the coefficients
	pc.coeff = d.coeff.Unpack(raw, rHdrPos)

	// Store coded payload
	coeffsInHdr := d.coeff.CoeffFieldSize() // Coefficients in rQUIC header
	if coeffsInHdr < 0 {
		coeffsInHdr = (0 - coeffsInHdr) * pc.remaining
	}
	// The next line is necessary for Rx buffer correctly rescuing decoded pld
	raw[rHdrPos+rquic.FieldPosGenSize] = uint8(coeffsInHdr)
	pc.pld = raw[rHdrPos+rquic.FieldPosSeed+coeffsInHdr:]
	pc.codedLen = pc.pld[:rquic.LenOfSrcLen]
	pc.codedPld = pc.pld[rquic.LenOfSrcLen:]

	// Remove existing SRC from this new COD
	if srcs, inds, genNotFull := d.optimizeThisCodAim(pc); genNotFull {
		if d.optimizeThisCodFire(pc, srcs, inds) { // COD is useful
			d.pktsCod = append(d.pktsCod, pc) // Store new parsed COD
		}
	}
}

func MakeDecoder() *Decoder {
	return &Decoder{ // if d.pollutionCount < 0 --> Close this path/connection
		// if COD --> d.pollutionCount -= rquic.MinRate
		// if SRC --> d.pollutionCount++
		// TODO: implement pollution attack detection
		pollutionCount: rquic.MinRatio * rquic.RxRedunMarg,

		srcAvbl: make([]byte, 0, (1+rquic.GenMargin)*rquic.MaxGenSize),
		srcMiss: make([]byte, 0, (1+rquic.GenMargin)*rquic.MaxGenSize),
	}
}
