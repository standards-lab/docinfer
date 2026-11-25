package docinfer

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
)

const (
	pngSignature = "\x89PNG\r\n\x1a\n"
	pngIHDREnd   = len(pngSignature) + 4 + 4 + 13 + 4
	jpegExif     = "Exif\x00\x00"
	jpegICC      = "ICC_PROFILE\x00"
)

var pngCarried = map[string]bool{"iCCP": true, "sRGB": true, "gAMA": true, "cHRM": true, "eXIf": true}

func carryJPEG(src, dst []byte, gray bool) []byte {
	var exif, icc [][]byte
	var profile []byte
	for i := 2; ; {
		for i < len(src) && src[i] == 0xff && i+1 < len(src) && src[i+1] == 0xff {
			i++
		}
		if i+4 > len(src) || src[i] != 0xff {
			return dst
		}
		marker := src[i+1]
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			i += 2
			continue
		}
		if marker == 0xda || marker == 0xd9 {
			break
		}
		n := int(binary.BigEndian.Uint16(src[i+2:]))
		if n < 2 || i+2+n > len(src) {
			return dst
		}
		seg, body := src[i:i+2+n], src[i+4:i+2+n]
		switch {
		case marker == 0xe1 && bytes.HasPrefix(body, []byte(jpegExif)):
			exif = append(exif, seg)
		case marker == 0xe2 && bytes.HasPrefix(body, []byte(jpegICC)):
			icc = append(icc, seg)
			if c := body[len(jpegICC):]; len(c) >= 2 && c[0] == 1 {
				profile = c[2:]
			}
		}
		i += 2 + n
	}
	if !iccFits(profile, gray) {
		icc = nil
	}
	if len(dst) < 2 || len(exif)+len(icc) == 0 {
		return dst
	}
	out := append([]byte(nil), dst[:2]...)
	for _, seg := range append(exif, icc...) {
		out = append(out, seg...)
	}
	return append(out, dst[2:]...)
}

func carryPNG(src, dst []byte, gray bool) []byte {
	if !bytes.HasPrefix(src, []byte(pngSignature)) || len(dst) < pngIHDREnd {
		return dst
	}
	var chunks [][]byte
	for i := len(pngSignature); ; {
		if i+12 > len(src) {
			return dst
		}
		n := int(binary.BigEndian.Uint32(src[i:]))
		if n < 0 || n > len(src)-i-12 {
			return dst
		}
		kind := string(src[i+4 : i+8])
		if kind == "IDAT" || kind == "IEND" {
			break
		}
		if pngCarried[kind] && (kind != "iCCP" || iccFits(iccpProfile(src[i+8:i+8+n]), gray)) {
			chunks = append(chunks, src[i:i+12+n])
		}
		i += 12 + n
	}
	if len(chunks) == 0 {
		return dst
	}
	out := append([]byte(nil), dst[:pngIHDREnd]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, dst[pngIHDREnd:]...)
}

func iccpProfile(data []byte) []byte {
	name := bytes.IndexByte(data, 0)
	if name < 1 || name+2 > len(data) || data[name+1] != 0 {
		return nil
	}
	r, err := zlib.NewReader(bytes.NewReader(data[name+2:]))
	if err != nil {
		return nil
	}
	header := make([]byte, 20)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil
	}
	return header
}

func iccFits(profile []byte, gray bool) bool {
	if len(profile) < 20 {
		return false
	}
	space := string(profile[16:20])
	if gray {
		return space == "GRAY"
	}
	return space == "RGB "
}
