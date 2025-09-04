package compression

import (
	"encoding/binary"
	"math"
)

type BitWriter struct {
	data    []byte
	current byte
	bitPos  uint8
}

func NewBitWriter() *BitWriter {
	return &BitWriter{
		data:   make([]byte, 0),
		bitPos: 0,
	}
}

func (bw *BitWriter) WriteBits(value uint64, numBits uint8) {
	for numBits > 0 {
		bitsLeft := 8 - bw.bitPos
		if numBits >= bitsLeft {
			bw.current |= byte(value>>(numBits-bitsLeft)) & ((1 << bitsLeft) - 1)
			bw.data = append(bw.data, bw.current)
			bw.current = 0
			numBits -= bitsLeft
			bw.bitPos = 0
		} else {
			shift := bitsLeft - numBits
			bw.current |= byte(value<<shift) & ((1 << bitsLeft) - 1)
			bw.bitPos += numBits
			numBits = 0
		}
	}
}

func (bw *BitWriter) Flush() []byte {
	if bw.bitPos > 0 {
		bw.data = append(bw.data, bw.current)
	}
	return bw.data
}

func CompressFloat64(values []float64) []byte {
	if len(values) == 0 {
		return []byte{}
	}

	bw := NewBitWriter()
	
	firstBits := math.Float64bits(values[0])
	bw.WriteBits(firstBits, 64)

	if len(values) == 1 {
		return bw.Flush()
	}

	prevValue := firstBits
	prevLeading := uint8(0)
	prevTrailing := uint8(0)

	for i := 1; i < len(values); i++ {
		curValue := math.Float64bits(values[i])
		xor := curValue ^ prevValue

		if xor == 0 {
			bw.WriteBits(0, 1)
		} else {
			bw.WriteBits(1, 1)

			leading := uint8(countLeadingZeros(xor))
			trailing := uint8(countTrailingZeros(xor))
			
			if leading >= prevLeading && trailing >= prevTrailing {
				bw.WriteBits(0, 1)
				meaningfulBits := 64 - prevLeading - prevTrailing
				bw.WriteBits(xor>>prevTrailing, meaningfulBits)
			} else {
				bw.WriteBits(1, 1)
				bw.WriteBits(uint64(leading), 6)
				meaningfulBits := 64 - leading - trailing
				bw.WriteBits(uint64(meaningfulBits), 6)
				bw.WriteBits(xor>>trailing, meaningfulBits)
				
				prevLeading = leading
				prevTrailing = trailing
			}
		}
		prevValue = curValue
	}

	return bw.Flush()
}

func countLeadingZeros(x uint64) int {
	if x == 0 {
		return 64
	}
	n := 0
	if x <= 0x00000000FFFFFFFF {
		n += 32
		x <<= 32
	}
	if x <= 0x0000FFFFFFFFFFFF {
		n += 16
		x <<= 16
	}
	if x <= 0x00FFFFFFFFFFFFFF {
		n += 8
		x <<= 8
	}
	if x <= 0x0FFFFFFFFFFFFFFF {
		n += 4
		x <<= 4
	}
	if x <= 0x3FFFFFFFFFFFFFFF {
		n += 2
		x <<= 2
	}
	if x <= 0x7FFFFFFFFFFFFFFF {
		n += 1
	}
	return n
}

func countTrailingZeros(x uint64) int {
	if x == 0 {
		return 64
	}
	n := 0
	if (x & 0x00000000FFFFFFFF) == 0 {
		n += 32
		x >>= 32
	}
	if (x & 0x000000000000FFFF) == 0 {
		n += 16
		x >>= 16
	}
	if (x & 0x00000000000000FF) == 0 {
		n += 8
		x >>= 8
	}
	if (x & 0x000000000000000F) == 0 {
		n += 4
		x >>= 4
	}
	if (x & 0x0000000000000003) == 0 {
		n += 2
		x >>= 2
	}
	if (x & 0x0000000000000001) == 0 {
		n += 1
	}
	return n
}

type BitReader struct {
	data   []byte
	pos    int
	bitPos uint8
}

func NewBitReader(data []byte) *BitReader {
	return &BitReader{
		data:   data,
		pos:    0,
		bitPos: 0,
	}
}

func (br *BitReader) ReadBits(numBits uint8) (uint64, bool) {
	if br.pos >= len(br.data) {
		return 0, false
	}

	var result uint64
	for numBits > 0 {
		if br.pos >= len(br.data) {
			return 0, false
		}

		bitsLeft := 8 - br.bitPos
		if numBits >= bitsLeft {
			mask := byte((1 << bitsLeft) - 1)
			bits := (br.data[br.pos] >> (8 - bitsLeft - br.bitPos)) & mask
			result = (result << bitsLeft) | uint64(bits)
			numBits -= bitsLeft
			br.pos++
			br.bitPos = 0
		} else {
			shift := bitsLeft - numBits
			mask := byte((1 << numBits) - 1)
			bits := (br.data[br.pos] >> shift) & mask
			result = (result << numBits) | uint64(bits)
			br.bitPos += numBits
			numBits = 0
		}
	}
	return result, true
}

func DecompressFloat64(data []byte, count int) []float64 {
	if len(data) == 0 || count == 0 {
		return []float64{}
	}

	br := NewBitReader(data)
	result := make([]float64, 0, count)

	firstBits, ok := br.ReadBits(64)
	if !ok {
		return result
	}
	result = append(result, math.Float64frombits(firstBits))

	if count == 1 {
		return result
	}

	prevValue := firstBits
	prevLeading := uint8(0)
	prevTrailing := uint8(0)

	for len(result) < count {
		controlBit, ok := br.ReadBits(1)
		if !ok {
			break
		}

		if controlBit == 0 {
			result = append(result, math.Float64frombits(prevValue))
		} else {
			blockType, ok := br.ReadBits(1)
			if !ok {
				break
			}

			var xor uint64
			if blockType == 0 {
				meaningfulBits := 64 - prevLeading - prevTrailing
				bits, ok := br.ReadBits(meaningfulBits)
				if !ok {
					break
				}
				xor = bits << prevTrailing
			} else {
				leading, ok := br.ReadBits(6)
				if !ok {
					break
				}
				meaningfulBits, ok := br.ReadBits(6)
				if !ok {
					break
				}
				bits, ok := br.ReadBits(uint8(meaningfulBits))
				if !ok {
					break
				}
				
				trailing := 64 - uint8(leading) - uint8(meaningfulBits)
				xor = bits << trailing
				
				prevLeading = uint8(leading)
				prevTrailing = trailing
			}

			prevValue = prevValue ^ xor
			result = append(result, math.Float64frombits(prevValue))
		}
	}

	return result
}

func WriteUint32(buf []byte, value uint32) {
	binary.LittleEndian.PutUint32(buf, value)
}

func WriteUint64(buf []byte, value uint64) {
	binary.LittleEndian.PutUint64(buf, value)
}

func signExtend(value int64, bits uint8) int64 {
	shift := 64 - bits
	return (value << shift) >> shift
}

func CompressInt64(values []int64) []byte {
	if len(values) == 0 {
		return []byte{}
	}

	bw := NewBitWriter()
	
	bw.WriteBits(uint64(values[0]), 64)

	if len(values) == 1 {
		return bw.Flush()
	}

	if len(values) == 2 {
		delta := values[1] - values[0]
		bw.WriteBits(uint64(delta), 64)
		return bw.Flush()
	}

	prevValue := values[1]
	prevDelta := values[1] - values[0]
	bw.WriteBits(uint64(prevDelta), 64)

	for i := 2; i < len(values); i++ {
		curValue := values[i]
		delta := curValue - prevValue
		deltaOfDelta := delta - prevDelta

		if deltaOfDelta == 0 {
			bw.WriteBits(0, 1)
		} else if deltaOfDelta >= -64 && deltaOfDelta <= 63 {
			bw.WriteBits(0b10, 2)
			encoded := uint64(deltaOfDelta & 0x7F)
			bw.WriteBits(encoded, 7)
		} else if deltaOfDelta >= -256 && deltaOfDelta <= 255 {
			bw.WriteBits(0b110, 3)
			encoded := uint64(deltaOfDelta & 0x1FF)
			bw.WriteBits(encoded, 9)
		} else if deltaOfDelta >= -2048 && deltaOfDelta <= 2047 {
			bw.WriteBits(0b1110, 4)
			encoded := uint64(deltaOfDelta & 0xFFF)
			bw.WriteBits(encoded, 12)
		} else {
			bw.WriteBits(0b1111, 4)
			bw.WriteBits(uint64(deltaOfDelta), 64)
		}

		prevValue = curValue
		prevDelta = delta
	}

	return bw.Flush()
}

func DecompressInt64(data []byte, count int) []int64 {
	if len(data) == 0 || count == 0 {
		return []int64{}
	}

	br := NewBitReader(data)
	result := make([]int64, 0, count)

	firstValue, ok := br.ReadBits(64)
	if !ok {
		return result
	}
	result = append(result, int64(firstValue))

	if count == 1 {
		return result
	}

	firstDelta, ok := br.ReadBits(64)
	if !ok {
		return result
	}
	prevValue := int64(firstValue) + int64(firstDelta)
	result = append(result, prevValue)

	if count == 2 {
		return result
	}

	prevDelta := int64(firstDelta)

	for len(result) < count {
		controlBit, ok := br.ReadBits(1)
		if !ok {
			break
		}

		var deltaOfDelta int64
		if controlBit == 0 {
			deltaOfDelta = 0
		} else {
			secondBit, ok := br.ReadBits(1)
			if !ok {
				break
			}

			if secondBit == 0 {
				bits, ok := br.ReadBits(7)
				if !ok {
					break
				}
				deltaOfDelta = signExtend(int64(bits), 7)
			} else {
				thirdBit, ok := br.ReadBits(1)
				if !ok {
					break
				}

				if thirdBit == 0 {
					bits, ok := br.ReadBits(9)
					if !ok {
						break
					}
					deltaOfDelta = signExtend(int64(bits), 9)
				} else {
					fourthBit, ok := br.ReadBits(1)
					if !ok {
						break
					}

					if fourthBit == 0 {
						bits, ok := br.ReadBits(12)
						if !ok {
							break
						}
						deltaOfDelta = signExtend(int64(bits), 12)
					} else {
						bits, ok := br.ReadBits(64)
						if !ok {
							break
						}
						deltaOfDelta = int64(bits)
					}
				}
			}
		}

		delta := prevDelta + deltaOfDelta
		value := prevValue + delta
		result = append(result, value)

		prevValue = value
		prevDelta = delta
	}

	return result
}

