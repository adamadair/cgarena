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
