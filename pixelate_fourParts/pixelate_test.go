package pixelate

import (
	"image"
	"image/png"
	"log"
	"os"
	"path"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sebdah/goldie/v2"
)

// go test -v ./pixelate_fourParts -update -run TestName

// Helpers
func readImagesFileinfo(image_dir string, file_paths chan string, num_images int) {

	f, err := os.Open(image_dir)
	if err != nil {
		log.Fatalln(err)
	}
	defer f.Close()

	files, err := f.Readdir(0)
	if err != nil {
		log.Fatalln(err)
	}

	for i, fileinfo := range files {
		if i == num_images {
			break
		}

		file_paths <- path.Join(image_dir, fileinfo.Name())
	}
}

func writeDoneImages(output_dir string, done_images chan *image.RGBA) {

	for img := range done_images {

		new_filepath := path.Join(output_dir, uuid.NewString())

		f, err := os.Create(new_filepath)
		if err != nil {
			log.Fatalln(err)
		}
		png.Encode(f, img)
		f.Close()
	}
}

// Tests
func TestPixelateTiming(t *testing.T) {

	chunk_size := 10
	num_images := 1000

	image_dir := "/home/chris/Documents/images/facenet/"

	var av_time int64 = 0
	loops := 30
	for i := 0; i < loops; i++ {

		file_paths := make(chan string, num_images)
		readImagesFileinfo(image_dir, file_paths, num_images)

		start := time.Now()
		Pixelate(chunk_size, file_paths)
		av_time += time.Since(start).Milliseconds()
	}
	av_time_f := float64(av_time) / float64(loops)
	t.Log("average time (ms): ", av_time_f)
}

func TestGoldens(t *testing.T) {
	tests := map[string]struct {
		input  string
		golden string
	}{
		"example1": {input: "example_golden_output_1", golden: "example1"},
		"example2": {input: "example_golden_output_2", golden: "example2"},
		"example3": {input: "example_golden_output_3", golden: "example3"},
		"example4": {input: "example_golden_output_4", golden: "example4"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {

			got := tc.input

			g := goldie.New(t, goldie.WithDiffEngine(goldie.ColoredDiff))
			g.Assert(t, name, []byte(got))

		})
	}
}

func TestExample(t *testing.T) {

	chunk_size := 10
	num_images := 10

	image_dir := "/home/chris/Documents/images/facenet/"
	output_dir := "/home/chris/Documents/images/output/"
	file_paths := make(chan string, num_images)

	readImagesFileinfo(image_dir, file_paths, num_images)

	done_images := Pixelate(chunk_size, file_paths)

	writeDoneImages(output_dir, done_images)
}
