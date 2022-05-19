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
	img             *image.Image
	done_img_chunks *chan *Chunk
	processed_color *color.RGBA

	img_ht int
	img_wt int

	chunk_start_x int
	chunk_start_y int
	chunk_end_x   int
	chunk_end_y   int
	chunk_size    int
}

type WorkingImage struct {
	img             *image.Image
	done_img_chunks *chan *Chunk

	n_img_chunks int

	img_ht int
	img_wt int
}

func loadImageFiles(input_filepaths chan string, loadedImages chan *WorkingImage, control chan int, wg *sync.WaitGroup) {

	for file_path := range input_filepaths {

		file, err := os.Open(file_path)
		if err != nil {
			log.Fatalln(err)
		}

		img, _, err := image.Decode(file)
		if err != nil {
			log.Fatalln(err)
		}

		loadedImages <- &WorkingImage{img: &img}

		file.Close()
	}
	wg.Done()
}

func chunkImage(imgChunks chan *Chunk, loadedImages chan *WorkingImage, workingImages chan *WorkingImage, chunk_size int, control chan int, wg *sync.WaitGroup) {

	for {

		select {

		case working_img := <-loadedImages:

			imgBounds := (*working_img.img).Bounds()
			img_ht := imgBounds.Max.Y
			img_wt := imgBounds.Max.X
			n_img_chunks := (((img_ht - 1) / chunk_size) + 1) * (((img_wt - 1) / chunk_size) + 1)

			// channel onto which processed chunks for THIS IMAGE will be put
			img_chunks_out := make(chan *Chunk, n_img_chunks)

			working_img.n_img_chunks = n_img_chunks
			working_img.img_ht = img_ht
			working_img.img_wt = img_wt
			working_img.done_img_chunks = &img_chunks_out

			// the output for this image
			workingImages <- working_img

			for start_y := 0; start_y < img_ht; start_y += chunk_size {
				for start_x := 0; start_x < img_wt; start_x += chunk_size {

					imgChunks <- &Chunk{img: working_img.img, done_img_chunks: &img_chunks_out, img_ht: img_ht, img_wt: img_wt, chunk_start_x: start_x, chunk_start_y: start_y, chunk_size: chunk_size}
				}
			}

		case <-control:
			wg.Done()
			return
		}
	}

}

func crunchChunks(imgChunks chan *Chunk, control chan int, wg *sync.WaitGroup) {

	for {

		select {

		case chunk := <-imgChunks:

			// define the image region over which this chunk operates
			start_y := chunk.chunk_start_y
			start_x := chunk.chunk_start_x

			var end_y, end_x int
			if end_y = chunk.chunk_start_y + chunk.chunk_size; end_y >= chunk.img_ht {
				end_y = chunk.img_ht
			}
			if end_x = chunk.chunk_start_x + chunk.chunk_size; end_x >= chunk.img_wt {
				end_x = chunk.img_wt
			}

			chunk.chunk_end_y = end_y
			chunk.chunk_end_x = end_x

			// read pixel values and compute average for this region
			num_sum := float64((end_y - start_y) * (end_x - start_x))
			var r_sum, g_sum, b_sum, a_sum float64 = 0, 0, 0, 0

			for y := start_y; y < end_y; y++ {
				for x := start_x; x < end_x; x++ {

					pixel := (*chunk.img).At(x, y)
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

			chunk.processed_color = &color.RGBA{r_av, g_av, b_av, a_av}
			*chunk.done_img_chunks <- chunk

		case <-control:
			wg.Done()
			return
		}
	}

}

func assembleDoneImages(done_images chan *image.RGBA, working_images chan *WorkingImage, done chan int, control chan int, wg *sync.WaitGroup) {

	for {
		select {
		case working_image := <-working_images:

			// done_img_chunks is a channel of pointers to processed chunks for a given image
			done_img_chunks := *working_image.done_img_chunks

			img_ht := working_image.img_ht
			img_wt := working_image.img_wt
			n_img_chunks := working_image.n_img_chunks

			img_out := image.NewRGBA(image.Rect(0, 0, img_wt, img_ht))

			n_chunks_consumed := 0
			for {
				chunk := <-done_img_chunks

				for y := chunk.chunk_start_y; y < chunk.chunk_end_y; y++ {
					for x := chunk.chunk_start_x; x < chunk.chunk_end_x; x++ {
						img_out.Set(x, y, *chunk.processed_color)
					}
				}

				// check image chunks are all consumed; image done
				n_chunks_consumed++
				if n_chunks_consumed == n_img_chunks {
					done_images <- img_out
					done <- 1
					break
				}
			}

		case <-control:
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

	log.Println("pixelate four parts")

	n_images := len(input_filepaths)
	close(input_filepaths)

	// set buffer sizes
	n_buffered_images := n_images
	if n_buffered_images > 100 {
		n_buffered_images = 100
	}
	n_buffered_chunks := 1000000

	// set number of gophers for each task
	numLoaders := 58
	numChunkers := 58
	numChunkCrunchers := 58
	numImgAssemblers := 58
	numWorkers := numLoaders + numChunkers + numChunkCrunchers + numImgAssemblers
	// numWorkers := numChunkers + numChunkCrunchers + numImgAssemblers

	// create channels
	kill_signal := make(chan int, numWorkers)
	count_images_done := make(chan int, n_images)
	loadedImages := make(chan *WorkingImage, n_buffered_images)
	workingImages := make(chan *WorkingImage, n_buffered_images)
	imgChunks := make(chan *Chunk, n_buffered_chunks)
	done_images := make(chan *image.RGBA, n_images)

	// get rid of this
	wg := new(sync.WaitGroup)
	wg.Add(numWorkers)

	for i := 0; i < numLoaders; i++ {
		go loadImageFiles(input_filepaths, loadedImages, kill_signal, wg)
	}

	for i := 0; i < numChunkers; i++ {
		go chunkImage(imgChunks, loadedImages, workingImages, chunk_size, kill_signal, wg)
	}

	for i := 0; i < numChunkCrunchers; i++ {
		go crunchChunks(imgChunks, kill_signal, wg)
	}

	for i := 0; i < numImgAssemblers; i++ {
		go assembleDoneImages(done_images, workingImages, count_images_done, kill_signal, wg)
	}

	// block this main routine until all images are done
	i := 0
	for i = 0; i < n_images; i++ {
		<-count_images_done
	}

	// send kill signals to the kill_signal channel, to tell workers to exit
	i = 0
	for i = 0; i < numWorkers; i++ {
		kill_signal <- 1
	}
	wg.Wait()

	close(done_images)
	return done_images
}
