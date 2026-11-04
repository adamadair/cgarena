package main

import "testing"

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

func TestApplyPlayerNames(t *testing.T) {
	tests := []struct {
		name    string
		players []Player
		names   []string
		want    []Player
	}{
		{
			name:    "sets names for all four players",
			players: []Player{{Name: "Player1", Cmd: "c1"}, {Name: "Player2", Cmd: "c2"}, {Name: "Player3", Cmd: "c3"}, {Name: "Player4", Cmd: "c4"}},
			names:   []string{"A", "B", "C", "D"},
			want:    []Player{{Name: "A", Cmd: "c1"}, {Name: "B", Cmd: "c2"}, {Name: "C", Cmd: "c3"}, {Name: "D", Cmd: "c4"}},
		},
		{
			name:    "pn3 and pn4 change the name and leave the command alone",
			players: []Player{{Name: "Player1", Cmd: "c1"}, {Name: "Player2", Cmd: "c2"}, {Name: "Player3", Cmd: "c3"}, {Name: "Player4", Cmd: "c4"}},
			names:   []string{"", "", "C", "D"},
			want:    []Player{{Name: "Player1", Cmd: "c1"}, {Name: "Player2", Cmd: "c2"}, {Name: "C", Cmd: "c3"}, {Name: "D", Cmd: "c4"}},
		},
		{
			name:    "names for missing players are ignored",
			players: []Player{{Name: "Player1", Cmd: "c1"}, {Name: "Player2", Cmd: "c2"}},
			names:   []string{"A", "", "C", "D"},
			want:    []Player{{Name: "A", Cmd: "c1"}, {Name: "Player2", Cmd: "c2"}},
		},
		{
			name:    "empty names change nothing",
			players: []Player{{Name: "Player1", Cmd: "c1"}, {Name: "Player2", Cmd: "c2"}},
			names:   []string{"", "", "", ""},
			want:    []Player{{Name: "Player1", Cmd: "c1"}, {Name: "Player2", Cmd: "c2"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyPlayerNames(tt.players, tt.names...)
			if len(tt.players) != len(tt.want) {
				t.Fatalf("got %d players, want %d", len(tt.players), len(tt.want))
			}
			for i := range tt.want {
				if tt.players[i] != tt.want[i] {
					t.Errorf("player %d = %+v, want %+v", i+1, tt.players[i], tt.want[i])
				}
			}
		})
	}
}
