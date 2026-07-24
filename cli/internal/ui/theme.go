// Tema dos modos por cor (spec/CLI.md §2): AZUL organiza, VERMELHO executa,
// VERDE descansa. Cor nunca é o único sinal — todo uso vem acompanhado de
// símbolo ou texto — então desligar (NO_COLOR, pipe) não perde informação.
package ui

import "fmt"

type Theme struct {
	On bool
}

func (t Theme) paint(code, s string) string {
	if !t.On {
		return s
	}
	return fmt.Sprintf("\x1b[%sm%s\x1b[0m", code, s)
}

func (t Theme) Title(s string) string { return t.paint("1;34", s) } // negrito azul (modo organizar)
func (t Theme) Blue(s string) string  { return t.paint("34", s) }
func (t Theme) Red(s string) string   { return t.paint("31", s) }
func (t Theme) Green(s string) string { return t.paint("32", s) }
func (t Theme) Warn(s string) string  { return t.paint("33", s) }
func (t Theme) Bold(s string) string  { return t.paint("1", s) }
func (t Theme) Dim(s string) string   { return t.paint("2", s) }

// Link emite OSC 8 quando a cor está ligada — o texto vira link clicável no
// terminal (mesma técnica da "fonte" do cartão). Desligado (NO_COLOR, pipe),
// devolve o texto puro; aí o chamador mostra a URL à parte, para não perder o
// acesso ao link.
func (t Theme) Link(url, text string) string {
	if !t.On {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// Badge pinta a prioridade A/B/C mantendo o colchete como sinal redundante.
func (t Theme) Badge(priority string) string {
	label := "[" + priority + "]"
	switch priority {
	case "A":
		return t.Red(label)
	case "B":
		return t.Warn(label)
	default:
		return t.Dim(label)
	}
}
