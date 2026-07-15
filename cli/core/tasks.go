// Lista do dia (spec/SPEC.md §4.3) — RF02/RF03.
// Tarefas ativas ordenadas A→B→C (sem prioridade = C); empate por criação.
package core

import "slices"

const maxActiveA = 4

var priorityRank = map[string]int{"A": 0, "B": 1, "C": 2}

func rankOf(priority *string) int {
	if priority == nil {
		return priorityRank["C"]
	}
	return priorityRank[*priority]
}

type Task struct {
	Title    string  `json:"title"`
	ParentID *string `json:"parent_id"`
	DueDate  *string `json:"due_date"`
	Priority *string `json:"priority"`
	Done     bool    `json:"done"`
}

type TasksState struct {
	Tasks   map[string]Task `json:"tasks"`
	DayList []string        `json:"day_list"`
	Alerts  TaskAlerts      `json:"alerts"`
}

type TaskAlerts struct {
	TooManyA bool `json:"too_many_a"`
}

func init() {
	Register("tasks", func(events []Event, _ Params) (any, error) {
		return Tasks(events)
	})
}

func Tasks(events []Event) (TasksState, error) {
	type taskState struct {
		Task
		id      string
		seq     int
		deleted bool
	}
	tasks := map[string]*taskState{}
	var order []*taskState

	for _, event := range Normalize(events) {
		switch event.Type {
		case "task.created":
			pl, err := decodePayload[struct {
				TaskID   string  `json:"task_id"`
				Title    string  `json:"title"`
				ParentID *string `json:"parent_id"`
				DueDate  *string `json:"due_date"`
			}](event)
			if err != nil {
				return TasksState{}, err
			}
			task := &taskState{
				Task: Task{Title: pl.Title, ParentID: pl.ParentID, DueDate: pl.DueDate},
				id:   pl.TaskID,
				seq:  len(order),
			}
			tasks[pl.TaskID] = task
			order = append(order, task)

		case "task.prioritized":
			pl, err := decodePayload[struct {
				TaskID   string `json:"task_id"`
				Priority string `json:"priority"`
			}](event)
			if err != nil {
				return TasksState{}, err
			}
			if task, ok := tasks[pl.TaskID]; ok {
				priority := pl.Priority
				task.Priority = &priority // última repriorização vence
			}

		case "task.edited":
			pl, err := decodePayload[struct {
				TaskID  string  `json:"task_id"`
				Title   *string `json:"title"`
				DueDate *string `json:"due_date"`
			}](event)
			if err != nil {
				return TasksState{}, err
			}
			if task, ok := tasks[pl.TaskID]; ok {
				// last-writer-wins por campo: só os campos presentes mudam
				if pl.Title != nil {
					task.Title = *pl.Title
				}
				if pl.DueDate != nil {
					task.DueDate = pl.DueDate
				}
			}

		case "task.deleted":
			pl, err := decodePayload[struct {
				TaskID string `json:"task_id"`
			}](event)
			if err != nil {
				return TasksState{}, err
			}
			if task, ok := tasks[pl.TaskID]; ok {
				task.deleted = true // tombstone: eventos posteriores são ignorados
				delete(tasks, pl.TaskID)
			}

		case "task.completed":
			pl, err := decodePayload[struct {
				TaskID string `json:"task_id"`
			}](event)
			if err != nil {
				return TasksState{}, err
			}
			if task, ok := tasks[pl.TaskID]; ok {
				task.Done = true
			}
		}
	}

	var active []*taskState
	for _, task := range order {
		if !task.Done && !task.deleted {
			active = append(active, task)
		}
	}

	dayList := slices.Clone(active)
	slices.SortStableFunc(dayList, func(a, b *taskState) int {
		if dr := rankOf(a.Priority) - rankOf(b.Priority); dr != 0 {
			return dr
		}
		return a.seq - b.seq
	})

	activeA := 0
	for _, task := range active {
		if task.Priority != nil && *task.Priority == "A" {
			activeA++
		}
	}

	state := TasksState{
		Tasks:   make(map[string]Task, len(tasks)),
		DayList: []string{},
		Alerts:  TaskAlerts{TooManyA: activeA > maxActiveA},
	}
	for id, task := range tasks {
		state.Tasks[id] = task.Task
	}
	for _, task := range dayList {
		state.DayList = append(state.DayList, task.id)
	}
	return state, nil
}
