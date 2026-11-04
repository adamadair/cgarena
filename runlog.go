package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitCommit returns the HEAD commit of the git repository dir and whether
// its working tree has uncommitted changes (git status --porcelain is not
// empty).
func gitCommit(dir string) (commit string, dirty bool, err error) {
	head, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	status, err := gitOutput(dir, "status", "--porcelain")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(head), strings.TrimSpace(status) != "", nil
}

// gitOutput runs git -C dir args... and returns its stdout. The error
// includes git's stderr.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
				return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
			}
		}
		return "", fmt.Errorf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// resolveCommits sets Commit and Dirty of every player with a CommitDir, once
// at the start of a run, so a bot rebuilt during the run doesn't change what
// is recorded. Players without a CommitDir keep null. A git failure (not a
// repository, git missing, no commit yet) also leaves null and returns a
// warning line; it never fails the run.
func resolveCommits(players []Player) []string {
	var warnings []string
	for i := range players {
		p := &players[i]
		p.Commit, p.Dirty = nil, nil
		if p.CommitDir == "" {
			continue
		}
		commit, dirty, err := gitCommit(p.CommitDir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Warning: can't read the git commit of %s from %s, recording null: %v",
				p.Name, p.CommitDir, err))
			continue
		}
		p.Commit, p.Dirty = &commit, &dirty
	}
	return warnings
}

// appendRunLog appends s as one line of JSON to the run log at path,
// creating it if needed. The line is written with a single Write on a file
// opened with O_APPEND, so the file is never truncated and each run adds one
// complete line.
func appendRunLog(s RunSummary, path string) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
