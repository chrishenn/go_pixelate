package pixelate

import (
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Chunk struct {
	processed_color *color.RGBA

	chunk_start_x int
	chunk_start_y int
	chunk_end_x   int
	chunk_end_y   int
}

type ProcessedImage struct {
	img_filename string

	n_img_chunks             int
	processed_chunks_channel *chan *Chunk

	img_ht int
	img_wt int
}

func readImagesFileinfo(image_dir string, file_names chan string, num_images int) {

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

		file_names <- fileinfo.Name()
	}
}

func processImages(image_dir string, file_names chan string, control chan int, chunk_size int, output_ldimgs chan *ProcessedImage, wg *sync.WaitGroup) {

	for {
		select {

		case img_filename := <-file_names:

			// open an image file. calculate its bounds, num of chunks
			file_path := filepath.Join(image_dir, img_filename)

			file, err := os.Open(file_path)
			if err != nil {
				log.Fatalln(err)
			}

			img, _, err := image.Decode(file)
			if err != nil {
				log.Fatalln(err)
			}

			imgBounds := img.Bounds()
			img_ht := imgBounds.Max.Y
			img_wt := imgBounds.Max.X

			n_img_chunks := (((img_ht - 1) / chunk_size) + 1) * (((img_wt - 1) / chunk_size) + 1)

			// channel onto which processed chunks for THIS IMAGE will be put
			processed_chunks_channel := make(chan *Chunk, n_img_chunks)

			ldimg := &ProcessedImage{img_filename: img_filename, n_img_chunks: n_img_chunks, processed_chunks_channel: &processed_chunks_channel, img_ht: img_ht, img_wt: img_wt}

			output_ldimgs <- ldimg

			// calculate the processed value from each chunk and put the chunk on it's image's output channel
			for chunk_i := 0; chunk_i < n_img_chunks; chunk_i++ {

				block_row := (chunk_i*chunk_size - 1) / img_wt
				start_y := block_row * chunk_size
				start_x := (chunk_i*chunk_size - 1) % img_wt

				var end_y, end_x int
				if end_y = start_y + chunk_size; end_y > img_ht {
					end_y = img_ht
				}
				if end_x = start_x + chunk_size; end_x > img_wt {
					end_x = img_wt
				}

				num_sum := float64((end_y - start_y) * (end_x - start_x))
				var r_sum, g_sum, b_sum, a_sum float64 = 0, 0, 0, 0

				for y := start_y; y < end_y; y++ {
					for x := start_x; x < end_x; x++ {
						pixel := img.At(x, y)
						colr := color.RGBAModel.Convert(pixel).(color.RGBA)
						r_sum += float64(colr.R)
						g_sum += float64(colr.G)
						b_sum += float64(colr.B)
						a_sum += float64(colr.A)
					}
				}

				r_av := uint8(math.Round(r_sum / num_sum))
				g_av := uint8(math.Round(g_sum / num_sum))
				b_av := uint8(math.Round(b_sum / num_sum))
				a_av := uint8(math.Round(a_sum / num_sum))

				colr_av := &color.RGBA{R: r_av, G: g_av, B: b_av, A: a_av}
				chunk := Chunk{chunk_start_x: start_x, chunk_start_y: start_y, chunk_end_x: end_x, chunk_end_y: end_y, processed_color: colr_av}
				processed_chunks_channel <- &chunk
			}

			err = file.Close()
			if err != nil {
				return
			}

		case <-control:
			wg.Done()
			return
		}
	}
}

func writeDoneImages(output_dir string, output_ldimgs chan *ProcessedImage, done chan int, control chan int, wg *sync.WaitGroup) {

	for {
		select {
		case proc_img := <-output_ldimgs:

			// outchan is a channel of pointers to processed chunks for a given image
			outchan := proc_img.processed_chunks_channel

			new_filepath := filepath.Join(output_dir, proc_img.img_filename+"_proc.png")

			n_img_chunks := proc_img.n_img_chunks
			n_processed_chunks := 0

			img_out := image.NewRGBA(image.Rect(0, 0, proc_img.img_wt, proc_img.img_ht))
			f, err := os.Create(new_filepath)
			if err != nil {
				log.Fatalln(err)
			}

			success := false
			for {
				chunk := <-*outchan

				for y := chunk.chunk_start_y; y < chunk.chunk_end_y; y++ {
					for x := chunk.chunk_start_x; x < chunk.chunk_end_x; x++ {
						img_out.Set(x, y, *chunk.processed_color)
					}
				}

				// check image chunks are all consumed; image done
				n_processed_chunks++
				if n_processed_chunks == n_img_chunks {
					success = true
					break
				}
			}

			if success {
				png.Encode(f, img_out)
				f.Close()
				done <- 1
			} else {
				log.Fatalln("write done ERROR")

				f.Close()
				done <- 1
			}

		case <-control:
			wg.Done()
			return
		}
	}

}

func Pixelate(
	output_ldimgs chan *ProcessedImage,
	done, control chan int,
	file_names chan string,
	numProcessors, numImgWriters, num_images int,
	image_dir, output_dir string,
	chunk_size int) int64 {

	numWorkers := numProcessors + numImgWriters
	wg := new(sync.WaitGroup)
	wg.Add(numWorkers)

	start := time.Now()

	for i := 0; i < numProcessors; i++ {
		go processImages(image_dir, file_names, control, chunk_size, output_ldimgs, wg)
	}

	for i := 0; i < numImgWriters; i++ {
		go writeDoneImages(output_dir, output_ldimgs, done, control, wg)
	}

	// block this main routine until all images are done
	i := 0
	for i = 0; i < num_images; i++ {
		<-done
	}

	// send signals to the control channel, to tell workers to exit
	i = 0
	for i = 0; i < numWorkers; i++ {
		control <- 1
	}
	wg.Wait()

	return time.Since(start).Milliseconds()
}

func main() {

	image_dir := "/home/chris/Documents/images/facenet/"
	output_dir := "/home/chris/Documents/images/output/"

	chunk_size := 10
	num_images := 1000
	n_buffered_images := 10
	n_buffered_chunks := 1000000

	numProcessors := 100
	numImgWriters := 10

	control := make(chan int, numProcessors+numImgWriters)
	done := make(chan int, num_images)
	file_names := make(chan string, num_images)
	output_ldimgs := make(chan *ProcessedImage, n_buffered_images)

	log.Println("input images:", num_images)
	log.Println("n_buffered_images, n_buffered_chunks:", n_buffered_images, n_buffered_chunks)
	log.Println("numProcessors, numImgWriters:", numProcessors, numImgWriters)

	var av_time int64 = 0
	loops := 30
	for i := 0; i < loops; i++ {

		// put filenames in the file_names channel
		readImagesFileinfo(image_dir, file_names, num_images)

		// Pixelate main code with timing
		elapsed := Pixelate(
			output_ldimgs,
			done, control,
			file_names,
			numProcessors, numImgWriters, num_images,
			image_dir, output_dir,
			chunk_size)

		av_time += elapsed
	}
	av_time_f := float64(av_time) / float64(loops)

	log.Println("average time (ms): ", av_time_f)

}
