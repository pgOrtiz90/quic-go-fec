package schemes

import (
	"github.com/lucas-clemente/quic-go/rquic"
	"github.com/lucas-clemente/quic-go/rquic/rLogger"
)

//////////////////////////////////////////////////////////////////////// redunBuilder

type redunBuilderXor struct {
	scheme      uint8
	genSize     uint8
	posRQuicHdr int
	posPld      int
	codedPkt    []byte // only 1 pkt per gen
	codedPldLen int
	finished    bool
}

func (r *redunBuilderXor) AddSrc(src []byte) {
	if r.finished {
		return
	}
	srcLen := len(src)
	if srcLen > r.codedPldLen {
		rLogger.Logf("Encoder ERROR SrcPldLen:%d > CodPldLen:%d", srcLen, r.codedPldLen)
		return
	} // Packets that are filled here are max size

	// Add SRC
	cod := r.codedPkt[r.posPld:]
	if r.genSize > 0 {
		for i, v := range src {
			cod[i] ^= v
		}
		r.genSize++
		return
	}
	// The slice returned in packetBuffer is not clean.
	var i int
	for i = 0; i < srcLen; i++ {
		cod[i] = src[i]
	}
	for ; i < r.codedPldLen; i++ {
		cod[i] = 0
	}
	r.genSize++
}

func (r *redunBuilderXor) ReadyToSend(ratio float64) bool {
	if r.genSize >= rquic.MaxGenSize {
		return true
	}
	return float64(r.genSize) >= ratio
}

func (r *redunBuilderXor) Finish() int {
	r.codedPkt[r.posRQuicHdr+rquic.FieldPosGenSize] = r.genSize
	r.codedPkt[r.posRQuicHdr+rquic.FieldPosType] = r.scheme
	r.finished = true
	return 0
}

func (r *redunBuilderXor) SeedMaxFieldSize() uint8 { return 0 }
func (r *redunBuilderXor) Scheme() byte { return r.scheme }
func (r *redunBuilderXor) Reduns() int { return 1 }
func (r *redunBuilderXor) RHdrPos() int { return r.posRQuicHdr }
func (r *redunBuilderXor) UnusedCoeffSpace() int { return 0 }

func makeRedunBuilderXor(packets [][]byte, posRQuicHdr int) *redunBuilderXor {
	rb := redunBuilderXor{
		scheme:      rquic.SchemeXor,
		posRQuicHdr: posRQuicHdr,
		posPld:      posRQuicHdr + rquic.CodPreHeaderSize,
		codedPkt:    packets[0],
	}
	rb.codedPldLen = len(rb.codedPkt) - rb.posPld
	return &rb
}

//////////////////////////////////////////////////////////////////////// coeffUnpacker

type coeffUnpackerXor struct{}

func (c *coeffUnpackerXor) Unpack(raw []byte, offset int) (coeffs []uint8) {
	genSize := raw[offset+rquic.FieldPosGenSize]
	coeffs = make([]uint8, genSize)
	for i := range coeffs {
		coeffs[i] = uint8(1)
	}
	return
}

func (c *coeffUnpackerXor) CoeffFieldSize() int {
	return 0
}

func makeCoeffUnpackerXor() *coeffUnpackerXor {
	return &coeffUnpackerXor{}
}