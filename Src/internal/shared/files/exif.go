package files

import (
	"encoding/binary"
	"errors"
)

// orientation 读取 JPEG 里的 EXIF 方向. 没有该信息时返回 1.
func orientation(data []byte) int {
	n, err := readOrientation(data)
	if err != nil || n < 1 || n > 8 {
		return 1
	}
	return n
}

func readOrientation(data []byte) (int, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1, errors.New("不是 jpeg")
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1, errors.New("标记无效")
		}
		marker := data[i+1]
		if marker == 0xD9 || marker == 0xDA {
			return 1, nil
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD8) {
			i += 2
			continue
		}
		if i+4 > len(data) {
			return 1, nil
		}
		size := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if size < 2 || i+2+size > len(data) {
			return 1, nil
		}
		if marker == 0xE1 {
			payload := data[i+4 : i+2+size]
			if n, ok := orientationFromExif(payload); ok {
				return n, nil
			}
		}
		i += 2 + size
	}
	return 1, nil
}

func orientationFromExif(payload []byte) (int, bool) {
	if len(payload) < 14 || string(payload[:6]) != "Exif\x00\x00" {
		return 0, false
	}
	tiff := payload[6:]
	if len(tiff) < 8 {
		return 0, false
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0, false
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset < 0 || offset+2 > len(tiff) {
		return 0, false
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	entry := offset + 2
	for n := 0; n < count; n++ {
		if entry+12 > len(tiff) {
			return 0, false
		}
		tag := order.Uint16(tiff[entry : entry+2])
		typ := order.Uint16(tiff[entry+2 : entry+4])
		if tag == 0x0112 && typ == 3 {
			return int(order.Uint16(tiff[entry+8 : entry+10])), true
		}
		entry += 12
	}
	return 0, false
}
