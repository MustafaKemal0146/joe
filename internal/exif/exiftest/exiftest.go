// Package exiftest, testlerde kullanılmak üzere EXIF'li küçük sentetik JPEG üretir.
package exiftest

import "encoding/binary"

type ent struct {
	tag, typ uint16
	count    uint32
	val      []byte
}

func ascii(tag uint16, s string) ent {
	b := append([]byte(s), 0)
	return ent{tag, 2, uint32(len(b)), b}
}

func rats(tag uint16, deg, min, sec uint32) ent {
	b := make([]byte, 24)
	for i, v := range [][2]uint32{{deg, 1}, {min, 1}, {sec * 100, 100}} {
		binary.LittleEndian.PutUint32(b[i*8:], v[0])
		binary.LittleEndian.PutUint32(b[i*8+4:], v[1])
	}
	return ent{tag, 5, 3, b}
}

// JPEG üretici ve model bilgisi ile (isteğe bağlı) GPS içeren, yalnızca başlık niteliğinde bir JPEG döndürür.
// lat/lon derece-dakika-saniye ve yarım küre (N/S, E/W) olarak verilir; gps false ise konum yazılmaz.
func JPEG(make_, model, taken string, gps bool, latDMS [3]uint32, latRef string, lonDMS [3]uint32, lonRef string) []byte {
	bo := binary.LittleEndian
	buf := []byte{'I', 'I', 0, 0, 0, 0, 0, 0}
	bo.PutUint16(buf[2:], 42)
	bo.PutUint32(buf[4:], 8)
	write := func(es []ent, ptrs map[uint16]uint32) int {
		start := len(buf)
		buf = append(buf, make([]byte, 2+len(es)*12+4)...)
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
	ifd0 := []ent{ascii(0x010F, make_), ascii(0x0110, model), {0x8769, 4, 1, make([]byte, 4)}}
	if gps {
		ifd0 = append(ifd0, ent{0x8825, 4, 1, make([]byte, 4)})
	}
	exifIFD := []ent{ascii(0x9003, taken)}
	gpsIFD := []ent{ascii(1, latRef), rats(2, latDMS[0], latDMS[1], latDMS[2]), ascii(3, lonRef), rats(4, lonDMS[0], lonDMS[1], lonDMS[2])}
	ptrs := map[uint16]uint32{}
	for pass := 0; pass < 2; pass++ {
		buf = buf[:8]
		write(ifd0, ptrs)
		ptrs[0x8769] = uint32(write(exifIFD, nil))
		if gps {
			ptrs[0x8825] = uint32(write(gpsIFD, nil))
		}
	}
	seg := append([]byte("Exif\x00\x00"), buf...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(out[4:], uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, 0xFF, 0xD9)
}
