package api

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"strings"
)

func pixelateImageBytes(data []byte, blockSize int) ([]byte, error) {
	if blockSize < 4 {
		blockSize = 4
	}
	reader := bytes.NewReader(data)
	img, format, err := image.Decode(reader)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if blockSize > width/4 {
		blockSize = width / 4
	}
	if blockSize > height/4 {
		blockSize = height / 4
	}
	if blockSize < 2 {
		blockSize = 2
	}

	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, img, bounds.Min, draw.Src)

	for y := bounds.Min.Y; y < bounds.Max.Y; y += blockSize {
		for x := bounds.Min.X; x < bounds.Max.X; x += blockSize {
			endX := x + blockSize
			endY := y + blockSize
			if endX > bounds.Max.X {
				endX = bounds.Max.X
			}
			if endY > bounds.Max.Y {
				endY = bounds.Max.Y
			}

			var rSum, gSum, bSum, aSum uint64
			var count uint64
			for py := y; py < endY; py++ {
				for px := x; px < endX; px++ {
					r, g, b, a := img.At(px, py).RGBA()
					rSum += uint64(r)
					gSum += uint64(g)
					bSum += uint64(b)
					aSum += uint64(a)
					count++
				}
			}
			if count == 0 {
				continue
			}
			avg := color.RGBA64{
				R: uint16(rSum / count),
				G: uint16(gSum / count),
				B: uint16(bSum / count),
				A: uint16(aSum / count),
			}

			for py := y; py < endY; py++ {
				for px := x; px < endX; px++ {
					result.Set(px, py, avg)
				}
			}
		}
	}

	var buf bytes.Buffer
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		err = jpeg.Encode(&buf, result, &jpeg.Options{Quality: 85})
	default:
		err = png.Encode(&buf, result)
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func pixelateImageToDataURL(data []byte, blockSize int) (string, error) {
	pixelated, err := pixelateImageBytes(data, blockSize)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(pixelated)
	return "data:image/png;base64," + encoded, nil
}
