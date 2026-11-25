package media

import "encoding/binary"

func DashFileSize(head []byte) (size int64, ok, needMore bool) {
	var off int64
	n := int64(len(head))
	for {
		if off+8 > n {
			return 0, false, true
		}
		boxSize := int64(binary.BigEndian.Uint32(head[off:]))
		boxType := string(head[off+4 : off+8])
		hdr := int64(8)
		if boxSize == 1 {
			if off+16 > n {
				return 0, false, true
			}
			boxSize = int64(binary.BigEndian.Uint64(head[off+8:]))
			hdr = 16
		}
		if off == 0 && boxType != "ftyp" {
			return 0, false, false
		}
		if boxSize < hdr {
			return 0, false, false
		}
		switch boxType {
		case "moof", "mdat":
			return 0, false, false
		case "sidx":
			if off+boxSize > n {
				return 0, false, true
			}
			total, valid := sidxTotal(head[off+hdr:off+boxSize], off+boxSize)
			return total, valid, false
		}
		off += boxSize
	}
}

func sidxTotal(b []byte, end int64) (int64, bool) {
	if len(b) < 4 {
		return 0, false
	}
	version := b[0]
	p := 4 + 8
	var firstOffset int64
	if version == 0 {
		if len(b) < p+8 {
			return 0, false
		}
		firstOffset = int64(binary.BigEndian.Uint32(b[p+4:]))
		p += 8
	} else {
		if len(b) < p+16 {
			return 0, false
		}
		firstOffset = int64(binary.BigEndian.Uint64(b[p+8:]))
		p += 16
	}
	if len(b) < p+4 {
		return 0, false
	}
	count := int(binary.BigEndian.Uint16(b[p+2:]))
	p += 4
	if count == 0 || len(b) < p+12*count {
		return 0, false
	}
	total := end + firstOffset
	for i := 0; i < count; i++ {
		ref := binary.BigEndian.Uint32(b[p:])
		if ref&0x80000000 != 0 {
			return 0, false
		}
		total += int64(ref & 0x7fffffff)
		p += 12
	}
	return total, true
}
