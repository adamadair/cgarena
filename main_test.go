package main

import (
	"strings"
	"testing"
)

func TestPlayer_WinPct(t *testing.T) {
	type fields struct {
		Name   string
		Cmd    string
		Wins   int
		Losses int
		Ties   int
	}
	tests := []struct {
		name   string
		fields fields
		want   float64
	}{
		{
			name: "No games played",
			fields: fields{
				Name:   "Player1",
				Cmd:    "cmd1",
				Wins:   0,
				Losses: 0,
				Ties:   0,
			},
			want: 0.0,
		},
		{
			name: "Only wins",
			fields: fields{
				Name:   "Player2",
				Cmd:    "cmd2",
				Wins:   5,
				Losses: 0,
				Ties:   0,
			},
			want: 1.0,
		},
		{
			name: "Wins and losses",
			fields: fields{
				Name:   "Player3",
				Cmd:    "cmd3",
				Wins:   3,
				Losses: 2,
				Ties:   0,
			},
			want: 0.6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Player{
				Name:   tt.fields.Name,
				Cmd:    tt.fields.Cmd,
				Wins:   tt.fields.Wins,
				Losses: tt.fields.Losses,
				Ties:   tt.fields.Ties,
			}
			if got := p.WinPct(); got != tt.want {
				t.Errorf("WinPct() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyPlayerFlags(t *testing.T) {
	config := []Player{{Name: "A", Cmd: "a", CommitDir: "da"}, {Name: "B", Cmd: "b"}}
	type flags struct {
		explicit          []string
		cmds, names, dirs [maxPlayers]string
	}
	tests := []struct {
		name    string
		base    []Player
		flags   flags
		want    []Player
		wantErr string
	}{
		{
			name:  "no config: defaults with flag commands",
			flags: flags{explicit: []string{"p1", "p2"}, cmds: [maxPlayers]string{"c1", "c2"}},
			want:  []Player{{Name: "Player1", Cmd: "c1"}, {Name: "Player2", Cmd: "c2"}},
		},
		{
			name: "no config: four players with names and dirs",
			flags: flags{
				explicit: []string{"p1", "p2", "p3", "p4", "pn1", "pn2", "pn3", "pn4", "pc3"},
				cmds:     [maxPlayers]string{"c1", "c2", "c3", "c4"},
				names:    [maxPlayers]string{"A", "B", "C", "D"},
				dirs:     [maxPlayers]string{"", "", "d3", ""},
			},
			want: []Player{{Name: "A", Cmd: "c1"}, {Name: "B", Cmd: "c2"}, {Name: "C", Cmd: "c3", CommitDir: "d3"}, {Name: "D", Cmd: "c4"}},
		},
		{
			name: "config players are used without player flags",
			base: config,
			want: config,
		},
		{
			name:  "flag values that were not given explicitly are ignored",
			base:  config,
			flags: flags{cmds: [maxPlayers]string{"x", "y"}, names: [maxPlayers]string{"X"}},
			want:  config,
		},
		{
			name: "-p2, -pn2 and -pc1 override fields of config players",
			base: config,
			flags: flags{
				explicit: []string{"p2", "pn2", "pc1"},
				cmds:     [maxPlayers]string{"", "b2"},
				names:    [maxPlayers]string{"", "B2"},
				dirs:     [maxPlayers]string{"da2"},
			},
			want: []Player{{Name: "A", Cmd: "a", CommitDir: "da2"}, {Name: "B2", Cmd: "b2"}},
		},
		{
			name: "-p3 appends a player to the config players",
			base: config,
			flags: flags{
				explicit: []string{"p3", "pn3"},
				cmds:     [maxPlayers]string{"", "", "c3"},
				names:    [maxPlayers]string{"", "", "C"},
			},
			want: []Player{config[0], config[1], {Name: "C", Cmd: "c3"}},
		},
		{
			name:  "empty values are ignored",
			base:  config,
			flags: flags{explicit: []string{"p1", "pn1", "pc1", "pn3"}},
			want:  config,
		},
		{
			name:    "-pn3 without player 3",
			base:    config,
			flags:   flags{explicit: []string{"pn3"}, names: [maxPlayers]string{"", "", "C"}},
			wantErr: "-pn3 is given but there is no player 3",
		},
		{
			name:    "-pc4 without player 4",
			flags:   flags{explicit: []string{"pc4"}, dirs: [maxPlayers]string{"", "", "", "d"}},
			wantErr: "-pc4 is given but there is no player 4",
		},
		{
			name:    "-p4 without -p3",
			base:    config,
			flags:   flags{explicit: []string{"p4"}, cmds: [maxPlayers]string{"", "", "", "c4"}},
			wantErr: "-p4 is given but there is no player 3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explicit := map[string]bool{}
			for _, name := range tt.flags.explicit {
				explicit[name] = true
			}
			base := append([]Player(nil), tt.base...)
			got, err := applyPlayerFlags(base, explicit, tt.flags.cmds, tt.flags.names, tt.flags.dirs)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i].Name != tt.want[i].Name || got[i].Cmd != tt.want[i].Cmd || got[i].CommitDir != tt.want[i].CommitDir {
					t.Errorf("player %d = %+v, want %+v", i+1, got[i], tt.want[i])
				}
			}
			for i := range tt.base {
				if base[i] != tt.base[i] {
					t.Errorf("config player %d changed to %+v", i+1, base[i])
				}
			}
		})
	}
}
