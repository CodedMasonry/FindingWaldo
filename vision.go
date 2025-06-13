package main

import (
	"fmt"
	"image/color"

	"gocv.io/x/gocv"
)

type Vision struct {
	window     *gocv.Window
	img        *gocv.Mat
	classifier gocv.CascadeClassifier
	outline    color.RGBA
}

func NewVision() (v *Vision) {
	v = &Vision{}

	// open display window
	v.window = gocv.NewWindow("Face Detect")
	defer v.window.Close()

	// prepare image matrix
	img := gocv.NewMat()
	defer img.Close()

	// color for the rect when faces detected
	v.outline = color.RGBA{0, 0, 255, 0}

	// load classifier to recognize faces
	v.classifier = gocv.NewCascadeClassifier()
	defer v.classifier.Close()

	if !v.classifier.Load("data/haarcascade_frontalface_default.xml") {
		fmt.Println("Error reading cascade file: data/haarcascade_frontalface_default.xml")
		return
	}

	return
}
