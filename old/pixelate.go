package old

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
	img             *image.Image
	img_chunks_out  chan *Chunk
	processed_color *color.RGBA

	img_ht int
	img_wt int

	chunk_start_x int
	chunk_start_y int
	chunk_end_x   int
	chunk_end_y   int
	chunk_size    int
}

type LoadedImage struct {
	img          *image.Image
	img_filepath string
	img_filename string

	n_img_chunks int

	img_ht int
	img_wt int
}

type ChanWrapper struct {
	channel *chan *Chunk
	ldimg   *LoadedImage
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

func loadImageFiles(image_dir string, file_names chan string, loadedImages chan *LoadedImage, control chan int, wg *sync.WaitGroup) {

	for {
		select {

		case filename := <-file_names:

			filepath := filepath.Join(image_dir, filename)

			file, err := os.Open(filepath)
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

			loadedImages <- &LoadedImage{img: &img, img_filepath: filepath, img_filename: filename, img_ht: img_ht, img_wt: img_wt}

			file.Close()

		case <-control:
			wg.Done()
			return
		}
	}
}

func chunkImage(imgChunks chan *Chunk, output_channels chan *ChanWrapper, loadedImages chan *LoadedImage, chunk_size int, control chan int, wg *sync.WaitGroup) {

	for {

		select {

		case ldimg := <-loadedImages:

			img_ht := ldimg.img_ht
			img_wt := ldimg.img_wt

			n_img_chunks := (((img_ht - 1) / chunk_size) + 1) * (((img_wt - 1) / chunk_size) + 1)
			ldimg.n_img_chunks = n_img_chunks

			// channel onto which processed chunks for THIS IMAGE will be put
			img_chunks_out := make(chan *Chunk, n_img_chunks)

			// the output channel for this image goes into the channel of output channels, with image data
			output_channels <- &ChanWrapper{channel: &img_chunks_out, ldimg: ldimg}

			chunks_written := 0
			for start_y := 0; start_y < img_ht; start_y += chunk_size {
				for start_x := 0; start_x < img_wt; start_x += chunk_size {

					imgChunks <- &Chunk{img: ldimg.img, img_chunks_out: img_chunks_out, img_ht: img_ht, img_wt: img_wt, chunk_start_x: start_x, chunk_start_y: start_y, chunk_size: chunk_size}
					// log.Println("created chunk: ", start_y, start_x, n_img_chunks, chunks_written)
					chunks_written++
				}
			}

			// log.Println("wrote:", chunks_written, "chunks, and n_chunks_todo was: ", n_img_chunks)
			if chunks_written != n_img_chunks {
				log.Fatalln("wrote ", chunks_written, " and n_chunks_todo was: ", n_img_chunks)
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
				end_y = chunk.img_ht - 1
			}
			if end_x = chunk.chunk_start_x + chunk.chunk_size; end_x >= chunk.img_wt {
				end_x = chunk.img_wt - 1
			}

			chunk.chunk_end_x = end_x
			chunk.chunk_end_y = end_y

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
			chunk.img_chunks_out <- chunk

		case <-control:
			wg.Done()
			return
		}
	}

}

func writeDoneImages(output_dir string, output_channels chan *ChanWrapper, done chan int, control chan int, wg *sync.WaitGroup) {

	for {
		select {
		case chanwrap := <-output_channels:

			// outchan is a channel of pointers to processed chunks for a given image
			outchan := chanwrap.channel

			new_filepath := filepath.Join(output_dir, chanwrap.ldimg.img_filename+"_proc.png")
			img_ht := chanwrap.ldimg.img_ht
			img_wt := chanwrap.ldimg.img_wt

			n_img_chunks := chanwrap.ldimg.n_img_chunks
			n_chunks_consumed := 0

			img_out := image.NewRGBA(image.Rect(0, 0, img_wt, img_ht))
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
				n_chunks_consumed++
				if n_chunks_consumed == n_img_chunks {
					// log.Println("all chunks for img consumed")
					success = true
					break
				}
			}

			if success {
				// log.Println("write done success")
				// log.Println("wrote file to: ", new_filepath)

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

func timeTrack(start time.Time, name string) int64 {
	elapsed := time.Since(start)
	log.Printf("%s took %s", name, elapsed)

	return elapsed.Milliseconds()
}

func LoadChunkCrunchWrite(loadedImages chan *LoadedImage, imgChunks chan *Chunk, output_channels chan *ChanWrapper,
	done, control chan int,
	file_names chan string,
	numLoaders, numChunkers, numChunkCrunchers, numImgWriters, numWorkers, num_images int,
	image_dir, output_dir string,
	chunk_size int) int64 {

	start := time.Now()

	wg := new(sync.WaitGroup)
	wg.Add(numWorkers)

	for i := 0; i < numLoaders; i++ {
		go loadImageFiles(image_dir, file_names, loadedImages, control, wg)
	}

	for i := 0; i < numChunkers; i++ {
		go chunkImage(imgChunks, output_channels, loadedImages, chunk_size, control, wg)
	}

	for i := 0; i < numChunkCrunchers; i++ {
		go crunchChunks(imgChunks, control, wg)
	}

	for i := 0; i < numImgWriters; i++ {
		go writeDoneImages(output_dir, output_channels, done, control, wg)
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
	num_images := 100
	n_buffered_images := 100
	n_buffered_chunks := 1000000

	numLoaders := 58
	numChunkers := 58
	numChunkCrunchers := 58
	numImgWriters := 58
	numWorkers := numLoaders + numChunkers + numChunkCrunchers + numImgWriters

	control := make(chan int, numWorkers)
	done := make(chan int, num_images)

	file_names := make(chan string, num_images)

	loadedImages := make(chan *LoadedImage, n_buffered_images)
	output_channels := make(chan *ChanWrapper, n_buffered_images)

	imgChunks := make(chan *Chunk, n_buffered_chunks)

	log.Println("using", num_images, "input images")
	log.Println("n_buffered_images, n_buffered_chunks", n_buffered_images, n_buffered_chunks)
	log.Println(numLoaders, numChunkers, numChunkCrunchers, numImgWriters)

	var av_time int64 = 0
	loops := 30
	for i := 0; i < loops; i++ {

		// put filenames in the file_names channel
		readImagesFileinfo(image_dir, file_names, num_images)

		// run main code with timing
		elapsed := LoadChunkCrunchWrite(
			loadedImages, imgChunks, output_channels,
			done, control,
			file_names,
			numLoaders, numChunkers, numChunkCrunchers, numImgWriters, numWorkers, num_images,
			image_dir, output_dir,
			chunk_size)

		av_time += elapsed
	}
	av_time_f := float64(av_time) / float64(loops)

	log.Println("average time (ms): ", av_time_f)
	log.Println("over this many loops: ", loops)

}
