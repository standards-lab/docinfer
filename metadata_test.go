package docinfer_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"slices"
	"testing"
)

func iccProfile(space string) []byte {
	p := make([]byte, 132)
	binary.BigEndian.PutUint32(p, uint32(len(p)))
	copy(p[12:], "mntr")
	copy(p[16:], space)
	copy(p[20:], "XYZ ")
	copy(p[36:], "acsp")
	return p
}

func exifOrientation(o uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.BigEndian.PutUint16(tiff[18:], o)
	return tiff
}

func grayPicture(w, h int) image.Image {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	return img
}

func jpegSegment(marker byte, body []byte) []byte {
	seg := []byte{0xff, marker, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(body)+2))
	return append(seg, body...)
}

func withSegments(jpg []byte, segs ...[]byte) []byte {
	out := append([]byte(nil), jpg[:2]...)
	for _, s := range segs {
		out = append(out, s...)
	}
	return append(out, jpg[2:]...)
}

func jpegCarried(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var segs [][]byte
	for i := 2; i+4 <= len(data); {
		marker := data[i+1]
		if marker == 0xda {
			return segs
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		body := data[i+4 : i+2+n]
		if (marker == 0xe1 && bytes.HasPrefix(body, []byte("Exif\x00\x00"))) || (marker == 0xe2 && bytes.HasPrefix(body, []byte("ICC_PROFILE\x00"))) {
			segs = append(segs, data[i:i+2+n])
		}
		i += 2 + n
	}
	t.Fatal("no SOS in the JPEG")
	return nil
}

func pngChunk(kind string, data []byte) []byte {
	c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	c = append(c, kind...)
	c = append(c, data...)
	return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))
}

func iccpChunk(t *testing.T, profile []byte) []byte {
	t.Helper()
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	if _, err := w.Write(profile); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return pngChunk("iCCP", append([]byte("ICC\x00\x00"), z.Bytes()...))
}

const ihdrEnd = 8 + 25

func withChunks(png []byte, chunks ...[]byte) []byte {
	out := append([]byte(nil), png[:ihdrEnd]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, png[ihdrEnd:]...)
}

func pngCarried(data []byte) [][]byte {
	var chunks [][]byte
	for i := 8; i+12 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[i:]))
		kind := string(data[i+4 : i+8])
		if kind == "IDAT" {
			break
		}
		if slices.Contains([]string{"iCCP", "sRGB", "gAMA", "cHRM", "eXIf"}, kind) {
			chunks = append(chunks, data[i:i+12+n])
		}
		i += 12 + n
	}
	return chunks
}

func sentWithin(t *testing.T, sent []byte, format string, maxPixels int) {
	t.Helper()
	cfg, got, err := image.DecodeConfig(bytes.NewReader(sent))
	if err != nil {
		t.Fatalf("the image sent: %v", err)
	}
	if got != format || cfg.Width*cfg.Height > maxPixels || cfg.Width*cfg.Height < maxPixels/2 {
		t.Errorf("sent a %s of %d×%d, want a %s downscaled within %d", got, cfg.Width, cfg.Height, format, maxPixels)
	}
	if _, _, err := image.Decode(bytes.NewReader(sent)); err != nil {
		t.Errorf("the image sent doesn't decode: %v", err)
	}
}

func TestRunCarriesJPEGMetadata(t *testing.T) {
	profile := iccProfile("RGB ")
	exif := jpegSegment(0xe1, append([]byte("Exif\x00\x00"), exifOrientation(6)...))
	icc1 := jpegSegment(0xe2, append([]byte("ICC_PROFILE\x00\x01\x02"), profile[:70]...))
	icc2 := jpegSegment(0xe2, append([]byte("ICC_PROFILE\x00\x02\x02"), profile[70:]...))
	src := withSegments(encoded(t, "jpeg", picture(300, 200)), icc1, exif, icc2)
	sent := describeImage(t, src, "image/jpeg", 6000)
	sentWithin(t, sent, "jpeg", 6000)
	if got, want := jpegCarried(t, sent), [][]byte{exif, icc1, icc2}; !slices.EqualFunc(got, want, bytes.Equal) {
		t.Errorf("sent %d Exif and ICC segments, want the source's Exif then its 2 ICC chunks in order, byte for byte", len(got))
	}
	if !bytes.Equal(sent[2:2+len(exif)], exif) {
		t.Error("the Exif segment doesn't follow SOI")
	}
}

func TestRunCarriesPNGMetadata(t *testing.T) {
	iccp := iccpChunk(t, iccProfile("RGB "))
	gama := pngChunk("gAMA", binary.BigEndian.AppendUint32(nil, 45455))
	exif := pngChunk("eXIf", exifOrientation(6))
	other := pngChunk("tEXt", []byte("Comment\x00kept out"))
	src := withChunks(encoded(t, "png", picture(300, 200)), iccp, gama, other, exif)
	sent := describeImage(t, src, "image/png", 6000)
	sentWithin(t, sent, "png", 6000)
	if got, want := pngCarried(sent), [][]byte{iccp, gama, exif}; !slices.EqualFunc(got, want, bytes.Equal) {
		t.Errorf("sent %d color and Exif chunks before IDAT, want the source's iCCP, gAMA, and eXIf, byte for byte", len(got))
	}
	if bytes.Contains(sent, []byte("tEXt")) {
		t.Error("sent the source's tEXt chunk")
	}
}

func TestRunMetadataEdges(t *testing.T) {
	exif := jpegSegment(0xe1, append([]byte("Exif\x00\x00"), exifOrientation(6)...))
	gama := pngChunk("gAMA", binary.BigEndian.AppendUint32(nil, 45455))
	grayICC := jpegSegment(0xe2, append([]byte("ICC_PROFILE\x00\x01\x01"), iccProfile("GRAY")...))
	cases := []struct {
		name, format string
		src          []byte
		want         [][]byte
	}{
		{"a JPEG whose segments don't parse", "jpeg", withSegments(encoded(t, "jpeg", picture(300, 200)), []byte("junk"), exif), nil},
		{"a JPEG ICC chunk too short for a header", "jpeg", withSegments(encoded(t, "jpeg", picture(300, 200)), exif, jpegSegment(0xe2, []byte("ICC_PROFILE\x00\x01\x01abc"))), [][]byte{exif}},
		{"a gray ICC profile on a color JPEG", "jpeg", withSegments(encoded(t, "jpeg", picture(300, 200)), jpegSegment(0xe2, append([]byte("ICC_PROFILE\x00\x01\x01"), iccProfile("GRAY")...))), nil},
		{"a gray ICC profile on a gray JPEG", "jpeg", withSegments(encoded(t, "jpeg", grayPicture(300, 200)), grayICC), [][]byte{grayICC}},
		{"a PNG iCCP that doesn't inflate", "png", withChunks(encoded(t, "png", picture(300, 200)), pngChunk("iCCP", []byte("ICC\x00\x00not zlib")), gama), [][]byte{gama}},
		{"a CMYK ICC profile on a PNG", "png", withChunks(encoded(t, "png", picture(300, 200)), iccpChunk(t, iccProfile("CMYK"))), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sent := describeImage(t, tc.src, "image/"+tc.format, 6000)
			sentWithin(t, sent, tc.format, 6000)
			got := pngCarried(sent)
			if tc.format == "jpeg" {
				got = jpegCarried(t, sent)
			}
			if !slices.EqualFunc(got, tc.want, bytes.Equal) {
				t.Errorf("sent %d metadata segments, want %d", len(got), len(tc.want))
			}
		})
	}
}

func TestRunSendsMetadataImageAsIs(t *testing.T) {
	exif := jpegSegment(0xe1, append([]byte("Exif\x00\x00"), exifOrientation(6)...))
	src := withSegments(encoded(t, "jpeg", picture(300, 200)), exif)
	if sent := describeImage(t, src, "image/jpeg", 300*200); !bytes.Equal(sent, src) {
		t.Errorf("sent %d bytes, want the image's %d as they are", len(sent), len(src))
	}
}
