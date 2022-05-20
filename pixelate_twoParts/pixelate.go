package pixelate

import (
	"image"
	"image/color"
	_ "image/jpeg"
	"log"
	"math"
	"os"
	"sync"
)

type Chunk struct {
	processed_color *color.RGBA

	chunk_start_x int
	chunk_start_y int
	chunk_end_x   int
	chunk_end_y   int
}

type ProcessedImage struct {
	n_img_chunks    int
	img_chunks_chan chan *Chunk

	img_ht int
	img_wt int
}

func processImages(input_filepaths chan string, working_images chan *ProcessedImage, chunk_size int, kill_signals chan int, wg *sync.WaitGroup) {

	for file_path := range input_filepaths {

		// open an image file. calculate its bounds, num of chunks
		file, err := os.Open(file_path)
		if err != nil {
			log.Println("file_path", file_path)
			log.Fatalln(err)
		}

		img, _, err := image.Decode(file)
		if err != nil {
			log.Println("file_path", file_path)
			log.Fatalln(err)
		}

		err = file.Close()
		if err != nil {
			return
		}

		imgBounds := img.Bounds()
		img_ht := imgBounds.Max.Y
		img_wt := imgBounds.Max.X
		n_img_chunks := ((img_ht / chunk_size) + 1) * ((img_wt / chunk_size) + 1)

		// channel onto which processed chunks for THIS IMAGE will be put
		img_chunks_chan := make(chan *Chunk, n_img_chunks)
		working_images <- &ProcessedImage{n_img_chunks: n_img_chunks, img_chunks_chan: img_chunks_chan, img_ht: img_ht, img_wt: img_wt}

		// calculate the processed value from each chunk and put the chunk on it's image's output channel
		for chunk_i := 0; chunk_i < n_img_chunks; chunk_i++ {

			n_blocks_x := img_wt/chunk_size + 1
			start_x := (chunk_i % n_blocks_x) * chunk_size
			start_y := (chunk_i / n_blocks_x) * chunk_size

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
			img_chunks_chan <- &chunk
		}
		close(img_chunks_chan)
	}
	wg.Done()
}

func assembleDoneImages(working_images chan *ProcessedImage, done_images chan *image.RGBA, done chan int, kill_signals chan int, wg *sync.WaitGroup) {

	for {
		select {
		case proc_img := <-working_images:

			// img_chunks_chan is a channel of pointers to processed chunks for a given image
			img_chunks_chan := proc_img.img_chunks_chan
			n_img_chunks := proc_img.n_img_chunks

			img_out := image.NewRGBA(image.Rect(0, 0, proc_img.img_wt, proc_img.img_ht))
			n_processed_chunks := 0
			for chunk := range img_chunks_chan {

				for y := chunk.chunk_start_y; y < chunk.chunk_end_y; y++ {
					for x := chunk.chunk_start_x; x < chunk.chunk_end_x; x++ {
						img_out.Set(x, y, chunk.processed_color)
					}
				}

				// check image chunks are all consumed; image done
				n_processed_chunks++
				if n_processed_chunks == n_img_chunks {
					done_images <- img_out
					done <- 1
					break
				}
			}

		case <-kill_signals:
			wg.Done()
			return
		}
	}
}

// Pixelate
// chunk_size: region size of pixels to average, defining a square with sides of size chunk_size tiled from the top-left
func Pixelate(
	chunk_size int,
	input_filepaths chan string,

) chan *image.RGBA {

	log.Println("pixelate two parts")

	n_images := len(input_filepaths)
	close(input_filepaths)

	n_buffered_images := n_images
	if n_buffered_images > 100 {
		n_buffered_images = 100
	}

	numProcessors := 116
	numImgWriters := 116
	numWorkers := numProcessors + numImgWriters

	kill_signals := make(chan int, numWorkers)
	count_done := make(chan int, n_images)
	working_images := make(chan *ProcessedImage, n_buffered_images)
	done_images := make(chan *image.RGBA, n_images)

	wg := new(sync.WaitGroup)
	wg.Add(numWorkers)

	for i := 0; i < numProcessors; i++ {
		go processImages(input_filepaths, working_images, chunk_size, kill_signals, wg)
	}

	for i := 0; i < numImgWriters; i++ {
		go assembleDoneImages(working_images, done_images, count_done, kill_signals, wg)
	}

	// block this main routine until all images are done
	i := 0
	for i = 0; i < n_images; i++ {
		<-count_done
	}

	// send signals to the control channel, to tell workers to exit
	i = 0
	for i = 0; i < numWorkers; i++ {
		kill_signals <- 1
	}
	wg.Wait()

	close(done_images)
	return done_images
}
