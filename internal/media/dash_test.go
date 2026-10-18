package media

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func box(typ string, payload []byte) []byte {
	b := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(8+len(payload)))
	copy(b[4:], typ)
	return append(b, payload...)
}

func sidxV0(sizes ...uint32) []byte {
	p := make([]byte, 0, 24+12*len(sizes))
	p = append(p, 0, 0, 0, 0)                   // version 0, flags
	p = binary.BigEndian.AppendUint32(p, 1)     // reference_ID
	p = binary.BigEndian.AppendUint32(p, 44100) // timescale
	p = binary.BigEndian.AppendUint32(p, 0)     // earliest_presentation_time
	p = binary.BigEndian.AppendUint32(p, 0)     // first_offset
	p = binary.BigEndian.AppendUint16(p, 0)     // reserved
	p = binary.BigEndian.AppendUint16(p, uint16(len(sizes)))
	for _, s := range sizes {
		p = binary.BigEndian.AppendUint32(p, s)
		p = binary.BigEndian.AppendUint32(p, 44100) // subsegment_duration
		p = binary.BigEndian.AppendUint32(p, 0x90000000)
	}
	return box("sidx", p)
}

// fakeDash builds a file shaped like YouTube's format 140: ftyp, moov,
// sidx, then the segments sidx describes (sizes include moof+mdat).
func fakeDash(segments ...int) []byte {
	var f bytes.Buffer
	f.Write(box("ftyp", []byte("dash\x00\x00\x00\x00iso6mp41")))
	f.Write(box("moov", make([]byte, 100)))
	sizes := make([]uint32, len(segments))
	for i, n := range segments {
		sizes[i] = uint32(n)
	}
	f.Write(sidxV0(sizes...))
	for _, n := range segments {
		f.Write(box("moof", make([]byte, 16)))
		f.Write(box("mdat", make([]byte, n-24-8)))
	}
	return f.Bytes()
}

func TestDashFileSize(t *testing.T) {
	file := fakeDash(1000, 2000, 1500)
	size, ok, needMore := DashFileSize(file)
	if !ok || needMore || size != int64(len(file)) {
		t.Fatalf("DashFileSize(full) = %d, %v, %v; want %d, true, false", size, ok, needMore, len(file))
	}

	// Заголовок приходит по кусочкам — до конца sidx размер неизвестен.
	headerEnd := bytes.Index(file, []byte("moof")) - 4
	for _, cut := range []int{0, 5, 30, headerEnd - 1} {
		if _, ok, needMore := DashFileSize(file[:cut]); ok || !needMore {
			t.Errorf("DashFileSize(first %d bytes) = ok %v needMore %v; want false, true", cut, ok, needMore)
		}
	}
	if size, ok, _ := DashFileSize(file[:headerEnd]); !ok || size != int64(len(file)) {
		t.Errorf("DashFileSize(header only) = %d, %v; want %d", size, ok, len(file))
	}
}

func TestDashFileSize_NotDash(t *testing.T) {
	// Обычный mp4 (moov+mdat без sidx) — размер узнаем только в конце.
	var f bytes.Buffer
	f.Write(box("ftyp", []byte("M4A \x00\x00\x00\x00")))
	f.Write(box("moov", make([]byte, 50)))
	f.Write(box("mdat", make([]byte, 500)))
	if _, ok, needMore := DashFileSize(f.Bytes()); ok || needMore {
		t.Errorf("DashFileSize(plain mp4) = ok %v needMore %v; want false, false", ok, needMore)
	}
	if _, ok, needMore := DashFileSize([]byte("<html>not a video at all</html>")); ok || needMore {
		t.Errorf("DashFileSize(html) = ok %v needMore %v; want false, false", ok, needMore)
	}
}
