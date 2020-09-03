package schemes

import (
	"math/rand"

	"github.com/lucas-clemente/quic-go/rquic"
	"github.com/lucas-clemente/quic-go/rquic/gf"
	"github.com/lucas-clemente/quic-go/rquic/rLogger"
)

//////////////////////////////////////////////////////////////////////// redunBuilder

type redunBuilderRlcSys struct {
	scheme      uint8
	genSize     uint8
	posRQuicHdr int
	posCoeffs   int
	posNewCoeff int
	posPld      int
	codedPkts   [][]byte
	codedPldLen int
	redun       int // coded packets in this gen
	finished    bool
}

func newCoeff() uint8 {
	// rand.Seed(time.Now().UnixNano())
	return uint8(rand.Intn(rquic.MaxGf-1) + 1)
}

func (r *redunBuilderRlcSys) AddSrc(src []byte) {
	if r.finished {
		return
	}
	srcLen := len(src)
	if srcLen > r.codedPldLen {
		rLogger.Logf("Encoder ERROR SrcPldLen:%d > CodPldLen:%d", srcLen, r.codedPldLen)
		return
	} // Packets that are filled here are max size

	var cf uint8
	if r.genSize > 0 {
		for _, cod := range r.codedPkts {
			// Update coefficients
			cf = newCoeff()
			cod[r.posNewCoeff] = cf
			// Add SRC
			cod = cod[r.posPld:]
			for i, v := range src {
				cod[i] ^= gf.Mult(v, cf)
			}
		}
		r.posNewCoeff++
		r.genSize++
		return
	}
	// The slice returned in packetBuffer is not clean.
	for _, cod := range r.codedPkts {
		// Update coefficients
		cf = newCoeff()
		cod[r.posNewCoeff] = cf
		// Add SRC
		cod = cod[r.posPld:]
		var i int
		for i = 0; i < srcLen; i++ {
			cod[i] = gf.Mult(src[i], cf)
		}
		for ; i < r.codedPldLen; i++ {
			cod[i] = 0
		}

	}
	r.posNewCoeff++
	r.genSize++
}

func (r *redunBuilderRlcSys) ReadyToSend(ratio float64) bool {
	if r.genSize >= rquic.MaxGenSize {
		return true
	}
	return float64(r.genSize)/float64(r.redun) >= ratio
}

func (r *redunBuilderRlcSys) Finish() int {
	unused := r.UnusedCoeffSpace()
	r.posRQuicHdr += unused
	posCoeffsNew := r.posCoeffs + unused
	posGenSize := r.posRQuicHdr + rquic.FieldPosGenSize
	posScheme := r.posRQuicHdr + rquic.FieldPosType

	for _, cod := range r.codedPkts {
		copy(cod[posCoeffsNew:r.posPld], cod[r.posCoeffs:r.posPld])
		cod[posGenSize] = r.genSize
		cod[posScheme] = r.scheme
	}

	r.finished = true
	return int(r.genSize)
}

func (r *redunBuilderRlcSys) SeedMaxFieldSize() uint8 { return rquic.MaxGenSize }
func (r *redunBuilderRlcSys) Scheme() byte { return r.scheme }
func (r *redunBuilderRlcSys) Reduns() int { return r.redun }
func (r *redunBuilderRlcSys) RHdrPos() int { return r.posRQuicHdr }
func (r *redunBuilderRlcSys) UnusedCoeffSpace() int { return int(rquic.MaxGenSize - r.genSize) }

func makeRedunBuilderRlcSys(packets [][]byte, posRQuicHdr int) *redunBuilderRlcSys {
	redun := len(packets)
	if redun == 0 {
		return nil
	}
	posCoeffs := posRQuicHdr + rquic.FieldPosSeed
	posPld := posCoeffs + int(rquic.MaxGenSize)
	return &redunBuilderRlcSys{
		scheme:      rquic.SchemeRlcSys,
		posRQuicHdr: posRQuicHdr,
		posCoeffs:   posCoeffs,
		posNewCoeff: posCoeffs,
		posPld:      posPld,
		codedPkts:   packets,
		codedPldLen: len(packets[0]) - posPld,
		redun:       redun,
	}
}

//////////////////////////////////////////////////////////////////////// coeffUnpacker

type coeffUnpackerRlcSys struct{}

func (c *coeffUnpackerRlcSys) Unpack(raw []byte, offset int) (coeffs []uint8) {
	genSize := int(raw[offset+rquic.FieldPosGenSize])
	coeffs = make([]uint8, genSize)
	cffsStart := offset + rquic.FieldPosSeed
	copy(coeffs, raw[cffsStart:cffsStart+genSize])
	return
}

func (c *coeffUnpackerRlcSys) CoeffFieldSize() int {
	return -1 // 1 * genSize
}

func makeCoeffUnpackerRlcSys() *coeffUnpackerRlcSys {
	return &coeffUnpackerRlcSys{}
}