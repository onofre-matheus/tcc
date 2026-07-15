// Mini-formulário sequencial: um campo por vez, Enter avança, campo
// obrigatório vazio não deixa passar. Usado na criação de cartão.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type formField struct {
	Label       string
	Placeholder string
	Value       string // conteúdo inicial (ex.: texto da nota na triagem)
	Required    bool
}

type miniForm struct {
	labels   []string
	required []bool
	inputs   []textinput.Model
	stage    int
}

func newMiniForm(fields ...formField) miniForm {
	form := miniForm{}
	for i, field := range fields {
		input := textinput.New()
		input.Prompt = " > "
		input.Placeholder = field.Placeholder
		input.SetValue(field.Value)
		if i == 0 {
			input.Focus()
		}
		form.labels = append(form.labels, field.Label)
		form.required = append(form.required, field.Required)
		form.inputs = append(form.inputs, input)
	}
	return form
}

// update devolve done=true quando o último campo é confirmado.
func (f miniForm) update(msg tea.Msg) (miniForm, bool, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyEnter {
		if f.required[f.stage] && strings.TrimSpace(f.inputs[f.stage].Value()) == "" {
			return f, false, nil
		}
		if f.stage == len(f.inputs)-1 {
			return f, true, nil
		}
		f.inputs[f.stage].Blur()
		f.stage++
		return f, false, f.inputs[f.stage].Focus()
	}

	var cmd tea.Cmd
	f.inputs[f.stage], cmd = f.inputs[f.stage].Update(msg)
	return f, false, cmd
}

func (f miniForm) values() []string {
	values := make([]string, len(f.inputs))
	for i, input := range f.inputs {
		values[i] = strings.TrimSpace(input.Value())
	}
	return values
}

func (f miniForm) view() string {
	var b strings.Builder
	for i := range f.inputs {
		b.WriteString(" " + f.labels[i] + "\n")
		b.WriteString(f.inputs[i].View() + "\n")
	}
	return b.String()
}
