package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Referee struct {
	Command   string
	Players   []Player
	Seed      int
	LogOutput string
}

type RefereeResult struct {
	Players []Player
	Scores  []int
	Seed    int
}

func (r *Referee) Run(matchId int) (*RefereeResult, error) {
	commandSplit := strings.Split(r.Command, " ")
	if len(commandSplit) == 0 {
		return nil, errors.New("no command to run") // No command to run
	}
	runCommand := commandSplit[0]

	var args []string

	if len(commandSplit) > 1 {
		args = append(args, commandSplit[1:]...)
	} else {
		args = []string{}
	}

	for i, player := range r.Players {
		args = append(args, "-p"+strconv.Itoa(i+1), player.Cmd)
	}

	if r.Seed > 0 {
		args = append(args, "-seed", strconv.Itoa(r.Seed+matchId))
	}

	if r.LogOutput != "" && r.LogOutput != "none" && r.LogOutput != "off" {
		logFile := filepath.Join(r.LogOutput, "match_"+strconv.Itoa(matchId)+".json")
		args = append(args, "-l", logFile)
	}

	cmd := exec.Command(runCommand, args...)
	var out strings.Builder
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}
	resultString := out.String()
	resultLines := strings.Split(resultString, "\n")
	scores := getScores(resultLines[:len(resultLines)-1]) // Exclude the last line which is the seed
	seed := getSeed(resultLines)
	resultPlayers := make([]Player, len(r.Players))
	for i, player := range r.Players {
		resultPlayers[i] = Player{
			Name:   player.Name,
			Cmd:    player.Cmd,
			Wins:   0, // Wins will be calculated later
			Losses: 0, // Losses will be calculated later
			Ties:   0, // Ties will be calculated later
		}
	}
	results := &RefereeResult{
		Players: resultPlayers,
		Scores:  scores,
		Seed:    seed,
	}
	results.ProcessResults()
	return results, nil
}

func getScores(lines []string) []int {
	var scores []int
	for _, line := range lines {
		i, err := strconv.Atoi(line)
		if err == nil {
			scores = append(scores, i)
		}
	}
	return scores
}

func getSeed(lines []string) int {
	for _, line := range lines {
		if strings.HasPrefix(line, "seed=") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				seed, _ := strconv.Atoi(parts[1])
				return seed
			}
		}
	}
	return 0 // Default seed if not found
}

func (rr *RefereeResult) ProcessResults() {
	if allEqual(rr.Scores) {
		for i := range rr.Players {
			rr.Players[i].Ties++
		}
	} else {
		maxScore := maxValue(rr.Scores)
		for i, score := range rr.Scores {
			if score == maxScore {
				rr.Players[i].Wins++
			} else {
				rr.Players[i].Losses++
			}
		}
	}
}

func allEqual(arr []int) bool {
	if len(arr) == 0 {
		return true
	}
	first := arr[0]
	for _, v := range arr[1:] {
		if v != first {
			return false
		}
	}
	return true
}

func maxValue(arr []int) int {
	if len(arr) == 0 {
		return 0
	}
	mx := arr[0]
	for _, v := range arr[1:] {
		if v > mx {
			mx = v
		}
	}
	return mx
}

func (rr *RefereeResult) String() string {
	var sb strings.Builder
	sb.WriteString("Referee Result:\n")
	for i, player := range rr.Players {
		sb.WriteString(player.Name + ": " + strconv.Itoa(rr.Scores[i]) + "; ")
	}
	sb.WriteString("Seed: " + strconv.Itoa(rr.Seed))
	return sb.String()
}
