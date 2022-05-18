package pixelate

import (
	"log"
	"testing"
	"time"
)

func TestPixelateTiming(t *testing.T) {

	image_dir := "/home/chris/Documents/images/facenet/"
	output_dir := "/home/chris/Documents/images/output/"

	chunk_size := 10
	num_images := 1000

	var av_time int64 = 0
	loops := 30
	for i := 0; i < loops; i++ {

		start := time.Now()

		Pixelate(
			image_dir,
			num_images,
			chunk_size,
			output_dir,
		)
		av_time += time.Since(start).Milliseconds()
	}
	av_time_f := float64(av_time) / float64(loops)
	log.Println("average time (ms): ", av_time_f)
}
