package media

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

func DecodeConfig(raw string) (image.Config, error) {
	rc, _, err := Open(raw)
	if err != nil {
		return image.Config{}, err
	}
	defer rc.Close()

	cfg, _, err := image.DecodeConfig(rc)
	return cfg, err
}

func Decode(raw string) (image.Image, error) {
	rc, _, err := Open(raw)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	img, _, err := image.Decode(rc)
	return img, err
}
