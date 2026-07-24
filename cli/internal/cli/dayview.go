// Tela do dia compartilhada por `pnn`/`pnn dia` e `procrastina`: monta as projeções,
// numera as tarefas em árvore (pais antes dos filhos, irmãos na ordem A→B→C da
// day_list) e persiste o mapa de números efêmeros.
package cli

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
)

type taskRow struct {
	num      int
	id       string
	depth    int
	hasChild bool
	task     core.Task
}

type dayView struct {
	today    string
	stats    core.StatsState
	tasks    core.TasksState
	leitner  core.LeitnerState
	inbox    core.InboxState
	calendar core.CalendarState
	rows     []taskRow
}

func buildDayView(a *app.App) (*dayView, error) {
	events, err := a.Store.Events()
	if err != nil {
		return nil, err
	}
	params := a.Params()

	view := &dayView{}
	if view.today, err = a.Today(); err != nil {
		return nil, err
	}
	if view.tasks, err = core.Tasks(events); err != nil {
		return nil, err
	}
	if view.leitner, err = core.Leitner(events, params); err != nil {
		return nil, err
	}
	if view.stats, err = core.Stats(events, params); err != nil {
		return nil, err
	}
	if view.inbox, err = core.Inbox(events); err != nil {
		return nil, err
	}
	if view.calendar, err = core.Calendar(events, params); err != nil {
		return nil, err
	}

	view.rows = taskTree(view.tasks)

	refs := make([]app.ViewRef, len(view.rows))
	for i, row := range view.rows {
		refs[i] = app.ViewRef{Kind: "task", ID: row.id}
	}
	if err := a.SaveView(refs); err != nil {
		return nil, err
	}
	return view, nil
}

// taskTree lineariza as tarefas ativas em profundidade: filhos logo abaixo do
// pai, irmãos na ordem da day_list. Filho de tarefa concluída vira raiz.
func taskTree(state core.TasksState) []taskRow {
	rank := make(map[string]int, len(state.DayList))
	for i, id := range state.DayList {
		rank[id] = i
	}

	children := map[string][]string{}
	var roots []string
	for _, id := range state.DayList {
		parent := state.Tasks[id].ParentID
		if parent != nil {
			if _, active := rank[*parent]; active {
				children[*parent] = append(children[*parent], id)
				continue
			}
		}
		roots = append(roots, id)
	}
	for _, ids := range children {
		slices.SortFunc(ids, func(a, b string) int { return rank[a] - rank[b] })
	}

	var rows []taskRow
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		rows = append(rows, taskRow{
			num:      len(rows) + 1,
			id:       id,
			depth:    depth,
			hasChild: len(children[id]) > 0,
			task:     state.Tasks[id],
		})
		for _, child := range children[id] {
			walk(child, depth+1)
		}
	}
	for _, id := range roots {
		walk(id, 0)
	}
	return rows
}

func (v *dayView) pendingInbox() int {
	return len(v.inbox.PendingNotes) + len(v.inbox.PendingDistractions)
}

// nextImportant devolve o próximo compromisso importante ainda por começar — o
// equivalente da CLI à "marca d'água" da extensão: como o terminal não notifica
// com a janela fechada, o lembrete aparece ao rodar `pnn`.
func (v *dayView) nextImportant(nowUTC string) (core.Appointment, bool) {
	now, err := time.Parse(time.RFC3339, nowUTC)
	if err != nil {
		return core.Appointment{}, false
	}
	for _, id := range sortedAppointmentIDs(v.calendar) {
		appt := v.calendar.Appointments[id]
		if appt.Importance != "important" {
			continue
		}
		if start, err := time.Parse(time.RFC3339, appt.StartsAt); err == nil && start.After(now) {
			return appt, true
		}
	}
	return core.Appointment{}, false
}

// untilLabelPT resume a antecedência em uma unidade ("em 6 dias", "em 2
// semanas"), como a marca d'água da extensão.
func untilLabelPT(now, start string) string {
	n, err1 := time.Parse(time.RFC3339, now)
	s, err2 := time.Parse(time.RFC3339, start)
	if err1 != nil || err2 != nil {
		return ""
	}
	d := s.Sub(n)
	if min := int(math.Round(d.Minutes())); min < 60 {
		return "em " + plural(min, "minuto", "minutos")
	}
	if h := int(math.Round(d.Hours())); h < 24 {
		return "em " + plural(h, "hora", "horas")
	}
	if days := int(math.Round(d.Hours() / 24)); days < 14 {
		return "em " + plural(days, "dia", "dias")
	}
	return "em " + plural(int(math.Round(d.Hours()/24/7)), "semana", "semanas")
}

func priorityLabel(task core.Task) string {
	if task.Priority == nil {
		return "C" // sem prioridade = C (spec/SPEC.md §4.3)
	}
	return *task.Priority
}

func (r taskRow) line(th ui.Theme) string {
	indent := ""
	if r.depth > 0 {
		indent = strings.Repeat(" ", r.depth) + th.Dim("↳") + " "
	}
	return fmt.Sprintf(" %d %s %s%s", r.num, th.Badge(priorityLabel(r.task)), indent, r.task.Title)
}

// sortedAppointmentIDs ordena os compromissos por (início, id), como o
// `upcoming` da projeção, mas sem filtrar os já encerrados.
func sortedAppointmentIDs(state core.CalendarState) []string {
	ids := make([]string, 0, len(state.Appointments))
	for id := range state.Appointments {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		if sa, sb := state.Appointments[a].StartsAt, state.Appointments[b].StartsAt; sa != sb {
			return strings.Compare(sa, sb)
		}
		return strings.Compare(a, b)
	})
	return ids
}

func localDateOf(tsUTC, tz string) (string, error) {
	return core.LocalDate(tsUTC, tz)
}

// --- datas e horas em pt-BR ---

var weekdaysPT = [...]string{"domingo", "segunda", "terça", "quarta", "quinta", "sexta", "sábado"}

var monthsPT = [...]string{
	"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
}

// longDatePT formata "2026-07-08" como "quarta, 8 de julho".
func longDatePT(isoDate string) string {
	t, err := time.Parse("2006-01-02", isoDate)
	if err != nil {
		return isoDate
	}
	return fmt.Sprintf("%s, %d de %s", weekdaysPT[t.Weekday()], t.Day(), monthsPT[t.Month()-1])
}

// localClock formata um instante UTC como HH:MM no fuso do usuário.
func localClock(tsUTC, tz string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return tsUTC
	}
	t, err := time.Parse(time.RFC3339, tsUTC)
	if err != nil {
		return tsUTC
	}
	return t.In(loc).Format("15:04")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
