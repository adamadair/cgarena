package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
)

type Arena struct {
	Referee   string   `json:"referee"`
	Players   []Player `json:"players"`
	Games     int      `json:"games"`
	Threads   int      `json:"threads"`
	Seed      int      `json:"seed"`
	LogOutput string   `json:"log_output"`
	Swap      bool     `json:"swap"` // Whether to switch players after each game
}

func (a *Arena) Run() error {
	// set up the player order based on the number of players and whether swapping is enabled
	var order [][]Player
	playerNum := len(a.Players)
	if a.Swap {
		if playerNum == 2 {
			order = [][]Player{
				{a.Players[0], a.Players[1]},
				{a.Players[1], a.Players[0]},
			}
		} else {
			order = playerPermutations(a.Players)
		}
	} else {
		order = [][]Player{a.Players}
	}

	if a.IsLoggingOutput() {
		clearLogDir(a.LogOutput)
	}

	var mu sync.Mutex

	var wg sync.WaitGroup
	jobs := make(chan int, a.Games)

	// Worker Goroutine Launch Loop
	for i := 0; i < a.Threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for matchID := range jobs {
				ref := Referee{
					Command:   a.Referee,
					Players:   a.Players, //order[matchID%len(order)],
					Seed:      a.Seed,
					LogOutput: a.LogOutput,
				}
				if a.Swap {
					ref.Players = order[matchID%len(order)]
				}
				result, err := ref.Run(matchID)
				if err != nil {
					log.Printf("Error running match %d: %v", matchID, err)
					continue
				}
				// Stub logic: award first player a win
				mu.Lock()
				// Update player stats based on the result
				for _, p := range result.Players {
					for j := range a.Players {
						if a.Players[j].Name == p.Name {
							a.Players[j].Wins += p.Wins
							a.Players[j].Losses += p.Losses
							a.Players[j].Ties += p.Ties
						}
					}
				}
				log.Printf("Match %d completed: %v", matchID, result.String())
				mu.Unlock()
			}
		}()
	}

	// Job dispatch loop
	for i := 0; i < a.Games; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	return nil
}

func (a *Arena) IsLoggingOutput() bool {
	return a.LogOutput != "" && a.LogOutput != "none" && a.LogOutput != "off"
}

func (a *Arena) PrintResults() string {
	var sb strings.Builder
	for _, player := range a.Players {
		sb.WriteString(player.Name + ": " + fmt.Sprintf("%.2f%% Win Rate; ", player.WinPct()*100) +
			strconv.Itoa(player.Wins) + " Wins; " +
			strconv.Itoa(player.Losses) + " Losses; " +
			strconv.Itoa(player.Ties) + " Ties\n")
	}
	return sb.String()
}

// use Heap's algorithm to generate all permutations of players
func playerPermutations(arr []Player) [][]Player {
	var res [][]Player
	var generate func([]Player, int)
	generate = func(a []Player, n int) {
		if n == 1 {
			tmp := make([]Player, len(a))
			copy(tmp, a)
			res = append(res, tmp)
			return
		}
		for i := 0; i < n; i++ {
			generate(a, n-1)
			if n%2 == 1 {
				a[0], a[n-1] = a[n-1], a[0]
			} else {
				a[i], a[n-1] = a[n-1], a[i]
			}
		}
	}
	generate(arr, len(arr))
	return res
}
