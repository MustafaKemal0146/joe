// Package exif, JPEG dosyalarının başındaki EXIF bloğundan cihaz, çekim zamanı ve GPS konumunu
// okur. Yalnızca bellekteki bir bayt dilimini ayrıştırır (görüntü çözülmez), her erişim sınır
// denetimlidir ve bozuk veri panik değil hata üretir.
package exif

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"time"
)

// Info bir fotoğrafın EXIF özetidir.
type Info struct {
	Make     string
	Model    string
	Software string
	// Taken çekim zamanıdır; EXIF yerel saat taşır, saat dilimi yoksa UTC varsayılır (HasTaken ile birlikte).
	Taken    time.Time
	HasTaken bool
	HasGPS   bool
	Lat, Lon float64
}

// ErrNoExif dosyada EXIF bloğu olmadığını bildirir (hata değil, olağan durum).
var ErrNoExif = errors.New("EXIF yok")

var errBad = errors.New("bozuk EXIF")

// Parse bir JPEG'in ilk baytlarından EXIF çıkarır (çoğu dosya için ilk 64 KiB yeterlidir).
func Parse(data []byte) (Info, error) {
	var info Info
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return info, ErrNoExif
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return info, ErrNoExif
		}
		marker := data[i+1]
		if marker == 0xD9 || marker == 0xDA { // EOI, SOS: meta veri bitti
			return info, ErrNoExif
		}
		if marker == 0xFF { // dolgu
			i++
			continue
		}
		size := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if size < 2 || i+2+size > len(data) {
			return info, errBad
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return parseTIFF(seg[6:])
		}
		i += 2 + size
	}
	return info, ErrNoExif
}

type tiff struct {
	b  []byte
	bo binary.ByteOrder
}

func (t tiff) u16(off int) (uint16, bool) {
	if off < 0 || off+2 > len(t.b) {
		return 0, false
	}
	return t.bo.Uint16(t.b[off:]), true
}

func (t tiff) u32(off int) (uint32, bool) {
	if off < 0 || off+4 > len(t.b) {
		return 0, false
	}
	return t.bo.Uint32(t.b[off:]), true
}

type entry struct {
	tag, typ uint16
	count    uint32
	valOff   int // değerin başladığı konum (satır içi ya da işaretli)
}

// entries bir IFD'nin girdilerini okur.
func (t tiff) entries(ifd int) ([]entry, error) {
	n, ok := t.u16(ifd)
	if !ok || n > 512 {
		return nil, errBad
	}
	out := make([]entry, 0, n)
	for k := 0; k < int(n); k++ {
		off := ifd + 2 + k*12
		tag, ok1 := t.u16(off)
		typ, ok2 := t.u16(off + 2)
		cnt, ok3 := t.u32(off + 4)
		if !ok1 || !ok2 || !ok3 || off+12 > len(t.b) {
			return nil, errBad
		}
		size := typeSize(typ) * int(cnt)
		valOff := off + 8
		if size > 4 {
			p, ok := t.u32(off + 8)
			if !ok {
				return nil, errBad
			}
			valOff = int(p)
		}
		out = append(out, entry{tag, typ, cnt, valOff})
	}
	return out, nil
}

func typeSize(t uint16) int {
	switch t {
	case 1, 2, 7:
		return 1
	case 3:
		return 2
	case 4, 9:
		return 4
	case 5, 10:
		return 8
	}
	return 0
}

func (t tiff) str(e entry) string {
	if e.typ != 2 || e.valOff < 0 || e.valOff+int(e.count) > len(t.b) || e.count > 256 {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(t.b[e.valOff:e.valOff+int(e.count)]), "\x00 "))
}

func (t tiff) rationals(e entry, n int) []float64 {
	if e.typ != 5 || int(e.count) < n || e.valOff < 0 || e.valOff+8*n > len(t.b) {
		return nil
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		num, _ := t.u32(e.valOff + i*8)
		den, _ := t.u32(e.valOff + i*8 + 4)
		if den == 0 {
			return nil
		}
		out[i] = float64(num) / float64(den)
	}
	return out
}

func parseTIFF(b []byte) (Info, error) {
	var info Info
	if len(b) < 8 {
		return info, errBad
	}
	t := tiff{b: b}
	switch string(b[:2]) {
	case "II":
		t.bo = binary.LittleEndian
	case "MM":
		t.bo = binary.BigEndian
	default:
		return info, errBad
	}
	if m, _ := t.u16(2); m != 42 {
		return info, errBad
	}
	ifd0, _ := t.u32(4)
	es, err := t.entries(int(ifd0))
	if err != nil {
		return info, err
	}
	var exifPtr, gpsPtr int
	for _, e := range es {
		switch e.tag {
		case 0x010F:
			info.Make = t.str(e)
		case 0x0110:
			info.Model = t.str(e)
		case 0x0131:
			info.Software = t.str(e)
		case 0x0132:
			if !info.HasTaken {
				info.Taken, info.HasTaken = parseTime(t.str(e))
			}
		case 0x8769:
			if v, ok := t.u32(e.valOff); ok {
				exifPtr = int(v)
			}
		case 0x8825:
			if v, ok := t.u32(e.valOff); ok {
				gpsPtr = int(v)
			}
		}
	}
	if exifPtr > 0 {
		if sub, err := t.entries(exifPtr); err == nil {
			for _, e := range sub {
				if e.tag == 0x9003 { // DateTimeOriginal
					if tm, ok := parseTime(t.str(e)); ok {
						info.Taken, info.HasTaken = tm, true
					}
				}
			}
		}
	}
	if gpsPtr > 0 {
		if g, err := t.entries(gpsPtr); err == nil {
			var latRef, lonRef string
			var lat, lon []float64
			for _, e := range g {
				switch e.tag {
				case 1:
					latRef = t.str(e)
				case 2:
					lat = t.rationals(e, 3)
				case 3:
					lonRef = t.str(e)
				case 4:
					lon = t.rationals(e, 3)
				}
			}
			if lat != nil && lon != nil {
				la := lat[0] + lat[1]/60 + lat[2]/3600
				lo := lon[0] + lon[1]/60 + lon[2]/3600
				if strings.EqualFold(latRef, "S") {
					la = -la
				}
				if strings.EqualFold(lonRef, "W") {
					lo = -lo
				}
				if !math.IsNaN(la) && !math.IsNaN(lo) && math.Abs(la) <= 90 && math.Abs(lo) <= 180 && !(la == 0 && lo == 0) {
					info.HasGPS, info.Lat, info.Lon = true, la, lo
				}
			}
		}
	}
	return info, nil
}

func parseTime(s string) (time.Time, bool) {
	if len(s) < 19 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006:01:02 15:04:05", s[:19])
	if err != nil || t.Year() < 1990 {
		return time.Time{}, false
	}
	return t, true
}
