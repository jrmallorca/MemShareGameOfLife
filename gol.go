package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Read = ioInput, Write = ioOutput
func readOrWritePgm(c ioCommand, p golParams, d distributorChans, world [][]byte, turns int) {
	switch c {
	// Request the io goroutine to read in the image with the given filename.
	case ioInput:
		d.io.command <- c
		d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight)}, "x")

	// Request the io goroutine to write image with given filename.
	case ioOutput:
		d.io.command <- c
		d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight), strconv.Itoa(turns)}, "x")

		// Send the finished state of the world to writePgmImage function
		d.io.worldState <- world
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell, state, pause, quit chan bool) {
	// Markers of which cells should be killed/resurrected
	var marked []cell
	var wg sync.WaitGroup
	var m sync.Mutex

	// Create the 2D slice to store the world.
	world := make([][]byte, p.imageHeight)
	for i := range world {
		world[i] = make([]byte, p.imageWidth)
	}

	// Read pgm image
	readOrWritePgm(ioInput, p, d, world, p.turns)

	// The io goroutine sends the requested image byte by byte, in rows.
	for y := 0; y < p.imageHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			val := <-d.io.inputVal
			if val != 0 {
				fmt.Println("Alive cell at", x, y)
				world[y][x] = val
			}
		}
	}

	// Initialize variables for y values
	yParams := make([]int, p.threads + 1)
	div := p.imageHeight/p.threads
	total := div * p.threads
	diff := 0

	// Source size of workers
	yParams[0] = 0
	for i := 1; i < p.threads + 1; i++  {
		yParams[i] = div
	}
	
	// Calculate, if not a power of two, and distribute evenly
	if total != p.imageHeight {
		diff = p.imageHeight - total
		for i := 1; i < diff + 1; i++  {
			yParams[i] += 1
		}
	}

	// Calculate y parameters
	for i := 1; i < p.threads + 1; i++  {
		yParams[i] += yParams[i - 1]
	}

	// Calculate the new state of Game of Life after the given number of turns.
	turns := 0
	currentAlive := 0
	timer := time.After(2 * time.Second)
	loop : for turns < p.turns {
		select {
		// Timer for every 2 seconds
		case <-timer:
			for y := 0; y < p.imageHeight; y++ {
				for x := 0; x < p.imageWidth; x++ {
					if world[y][x] != 0 {
						currentAlive++
					}
				}
			}

			fmt.Println("No. of cells alive: ", currentAlive)
			currentAlive = 0
			timer = time.After(2 * time.Second)

		case <-state:
			go readOrWritePgm(ioOutput, p, d, world, turns)

		case <-pause:
			go readOrWritePgm(ioOutput, p, d, world, turns)

			wg.Add(1)
			go func() {
				if <-pause {
					fmt.Println("Continuing")
					wg.Done()
				}
			}()
			wg.Wait()

		case <-quit:
			break loop

		default:
			turns++

			wg.Add(p.threads)

			for t := 0; t < p.threads; t++  {
				go func (startY, endY int) {
					defer wg.Done()

					for y := startY; y < endY; y++ {
						for x := 0; x < p.imageWidth; x++ {
							AliveCellsAround := 0

							// Check for how many alive cells are around the original cell (Ignore the original cell)
							// Adding the width and then modding it by them deals with out of bound issues
							for i := -1; i < 2; i++ {
								for j := -1; j < 2; j++ {
									if y + i == y && x + j == x {
										continue
									} else if world[((y + i) + p.imageHeight) % p.imageHeight][((x + j) + p.imageWidth) % p.imageWidth] == 0xFF {
										AliveCellsAround++
									}
								}
							}

							// Cases for alive and dead original cells
							// 'break' isn't needed for Golang switch
							switch world[y][x] {
							case 0xFF: // If cell alive
								if AliveCellsAround < 2 || AliveCellsAround > 3 {
									m.Lock()
									marked = append(marked, cell{x, y})
									m.Unlock()
								}
							case 0x00: // If cell dead
								if AliveCellsAround == 3 {
									m.Lock()
									marked = append(marked, cell{x, y})
									m.Unlock()
								}
							}
						}
					}
				}(yParams[t], yParams[t + 1])
			}
			wg.Wait()

			// Kill/resurrect those marked then reset contents of marked
			for _, cell := range marked {
				world[cell.y][cell.x] = world[cell.y][cell.x] ^ 0xFF
			}
			marked = nil
		}
	}

	// Create an empty slice to store coordinates of cells that are still alive after p.turns are done.
	var finalAlive []cell
	// Go through the world and append the cells that are still alive.
	for y := 0; y < p.imageHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			if world[y][x] != 0 {
				finalAlive = append(finalAlive, cell{x: x, y: y})
			}
		}
	}

	// Write image
	readOrWritePgm(ioOutput, p, d, world, turns)

	// Make sure that the Io has finished any output before exiting.
	d.io.command <- ioCheckIdle
	<-d.io.idle

	// Return the coordinates of cells that are still alive.
	alive <- finalAlive
}
