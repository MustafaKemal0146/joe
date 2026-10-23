package exif

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

type ent struct {
	tag, typ uint16
	count    uint32
	val      []byte // satır dışı ise veri; ≤4 bayt ise satır içi
	ptr      uint32 // IFD işaretçisi için (typ 4, count 1)
}

// buildJPEG küçük endian bir EXIF içeren sahte (yalnızca başlık) bir JPEG kurar.
func buildJPEG(ifd0 []ent, exifIFD []ent, gpsIFD []ent, be bool) []byte {
	var bo binary.ByteOrder = binary.LittleEndian
	hdr := []byte("II")
	if be {
		bo, hdr = binary.BigEndian, []byte("MM")
	}
	buf := append([]byte{}, hdr...)
	buf = append(buf, 0, 0, 0, 0, 0, 0)
	bo.PutUint16(buf[2:], 42)
	bo.PutUint32(buf[4:], 8)

	write := func(es []ent, ptrs map[uint16]uint32) int {
		start := len(buf)
		size := 2 + len(es)*12 + 4
		buf = append(buf, make([]byte, size)...)
		bo.PutUint16(buf[start:], uint16(len(es)))
		for i, e := range es {
			off := start + 2 + i*12
			bo.PutUint16(buf[off:], e.tag)
			bo.PutUint16(buf[off+2:], e.typ)
			bo.PutUint32(buf[off+4:], e.count)
			switch {
			case ptrs[e.tag] != 0:
				bo.PutUint32(buf[off+8:], ptrs[e.tag])
			case len(e.val) <= 4:
				copy(buf[off+8:off+12], e.val)
			default:
				bo.PutUint32(buf[off+8:], uint32(len(buf)))
				buf = append(buf, e.val...)
				if len(e.val)%2 == 1 {
					buf = append(buf, 0)
				}
			}
		}
		return start
	}
	// IFD0 en başta (konum 8): önce yer ayır, sonra alt IFD'leri ekleyip işaretçileri yaz.
	ptrs := map[uint16]uint32{}
	ifd0Start := len(buf)
	if ifd0Start != 8 {
		panic("IFD0 konumu 8 olmalı")
	}
	// IFD0 iki kez yazılır: ilk geçiş alt IFD konumlarını belirler, ikincisi işaretçileri doldurur.
	for pass := 0; pass < 2; pass++ {
		buf = buf[:8]
		write(ifd0, ptrs)
		if exifIFD != nil {
			ptrs[0x8769] = uint32(write(exifIFD, nil))
		}
		if gpsIFD != nil {
			ptrs[0x8825] = uint32(write(gpsIFD, nil))
		}
	}
	seg := append([]byte("Exif\x00\x00"), buf...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(out[4:], uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, 0xFF, 0xD9)
}

func asciiEnt(tag uint16, s string) ent {
	b := append([]byte(s), 0)
	return ent{tag: tag, typ: 2, count: uint32(len(b)), val: b}
}

func ratEnt(tag uint16, vals [][2]uint32, bo binary.ByteOrder) ent {
	b := make([]byte, 0, len(vals)*8)
	for _, v := range vals {
		x := make([]byte, 8)
		bo.PutUint32(x, v[0])
		bo.PutUint32(x[4:], v[1])
		b = append(b, x...)
	}
	return ent{tag: tag, typ: 5, count: uint32(len(vals)), val: b}
}

func sample(be bool) []byte {
	var bo binary.ByteOrder = binary.LittleEndian
	if be {
		bo = binary.BigEndian
	}
	ifd0 := []ent{
		asciiEnt(0x010F, "Apple"), asciiEnt(0x0110, "iPhone 13"), asciiEnt(0x0131, "16.4"),
		{tag: 0x8769, typ: 4, count: 1, val: make([]byte, 4)}, {tag: 0x8825, typ: 4, count: 1, val: make([]byte, 4)},
	}
	exifIFD := []ent{asciiEnt(0x9003, "2026:03:14 21:45:10")}
	gps := []ent{
		asciiEnt(1, "N"), ratEnt(2, [][2]uint32{{41, 1}, {0, 1}, {1800, 100}}, bo),
		asciiEnt(3, "E"), ratEnt(4, [][2]uint32{{28, 1}, {58, 1}, {3000, 100}}, bo),
	}
	return buildJPEG(ifd0, exifIFD, gps, be)
}

func TestParseDeviceTimeAndGPSBothByteOrders(t *testing.T) {
	for _, be := range []bool{false, true} {
		info, err := Parse(sample(be))
		if err != nil {
			t.Fatalf("be=%v: %v", be, err)
		}
		if info.Make != "Apple" || info.Model != "iPhone 13" || info.Software != "16.4" {
			t.Errorf("be=%v cihaz: %+v", be, info)
		}
		if !info.HasTaken || info.Taken.Format("2006-01-02 15:04:05") != "2026-03-14 21:45:10" {
			t.Errorf("be=%v zaman: %v", be, info.Taken)
		}
		// 41°0'18" N = 41.005, 28°58'30" E = 28.975
		if !info.HasGPS || math.Abs(info.Lat-41.005) > 1e-6 || math.Abs(info.Lon-28.975) > 1e-6 {
			t.Errorf("be=%v GPS: %+v", be, info)
		}
	}
}

func TestSouthWestSignsAndZeroGPS(t *testing.T) {
	bo := binary.LittleEndian
	ifd0 := []ent{{tag: 0x8825, typ: 4, count: 1, val: make([]byte, 4)}}
	gps := []ent{asciiEnt(1, "S"), ratEnt(2, [][2]uint32{{33, 1}, {0, 1}, {0, 1}}, bo), asciiEnt(3, "W"), ratEnt(4, [][2]uint32{{70, 1}, {30, 1}, {0, 1}}, bo)}
	info, err := Parse(buildJPEG(ifd0, nil, gps, false))
	if err != nil || !info.HasGPS || info.Lat != -33 || info.Lon != -70.5 {
		t.Fatalf("güney/batı işaretleri: %+v %v", info, err)
	}
	zero := []ent{asciiEnt(1, "N"), ratEnt(2, [][2]uint32{{0, 1}, {0, 1}, {0, 1}}, bo), asciiEnt(3, "E"), ratEnt(4, [][2]uint32{{0, 1}, {0, 1}, {0, 1}}, bo)}
	if info, _ := Parse(buildJPEG(ifd0, nil, zero, false)); info.HasGPS {
		t.Fatal("0,0 konumu (boş GPS) geçerli sayılmamalı")
	}
	div0 := []ent{asciiEnt(1, "N"), ratEnt(2, [][2]uint32{{1, 0}, {0, 1}, {0, 1}}, bo), asciiEnt(3, "E"), ratEnt(4, [][2]uint32{{1, 1}, {0, 1}, {0, 1}}, bo)}
	if info, _ := Parse(buildJPEG(ifd0, nil, div0, false)); info.HasGPS {
		t.Fatal("sıfıra bölme konum üretmemeli")
	}
}

func TestNoExifAndNotJPEG(t *testing.T) {
	for name, data := range map[string][]byte{
		"boş":        nil,
		"png":        []byte("\x89PNG\r\n\x1a\n"),
		"exifsiz":    {0xFF, 0xD8, 0xFF, 0xE0, 0, 4, 'J', 'F', 0xFF, 0xD9},
		"yalnız-sos": {0xFF, 0xD8, 0xFF, 0xDA, 0, 2},
	} {
		if _, err := Parse(data); err != ErrNoExif {
			t.Errorf("%s: ErrNoExif bekleniyordu: %v", name, err)
		}
	}
}

func TestMalformedNeverPanics(t *testing.T) {
	good := sample(false)
	for cut := 0; cut < len(good); cut++ { // her kesilmiş uzunluk
		Parse(good[:cut])
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 3000; i++ { // rastgele bozulmalar
		b := bytes.Clone(good)
		for k := 0; k < 1+r.Intn(6); k++ {
			b[r.Intn(len(b))] = byte(r.Intn(256))
		}
		Parse(b)
	}
	for i := 0; i < 500; i++ { // tamamen rastgele
		b := make([]byte, 20+r.Intn(200))
		r.Read(b)
		b[0], b[1] = 0xFF, 0xD8
		Parse(b)
	}
}
