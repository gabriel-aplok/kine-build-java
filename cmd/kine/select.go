package main

import (
	"fmt"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

type commandKind int

const (
	commandBuild commandKind = iota
	commandRun
	commandTest
	commandLint
	commandInstrument
	commandClean
	commandWatch
	commandDoctor
	commandTasks
	commandCache
	commandVersion
	commandHelp
	commandImport
)

func parseCommand(args []string) (commandKind, []string, error) {
	if len(args) == 0 {
		return commandBuild, nil, nil
	}

	commands := map[string]commandKind{
		"build":      commandBuild,
		"run":        commandRun,
		"test":       commandTest,
		"lint":       commandLint,
		"instrument": commandInstrument,
		"clean":      commandClean,
		"watch":      commandWatch,
		"doctor":     commandDoctor,
		"tasks":      commandTasks,
		"cache":      commandCache,
		"version":    commandVersion,
		"help":       commandHelp,
		"import":     commandImport,
	}
	if command, ok := commands[args[0]]; ok {
		rest := args[1:]
		if len(rest) == 0 {
			rest = nil
		}
		return command, rest, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return commandBuild, args, nil
	}
	return commandBuild, nil, fmt.Errorf("unknown command %q", args[0])
}

func commandName(command commandKind) string {
	switch command {
	case commandBuild:
		return "build"
	case commandRun:
		return "run"
	case commandTest:
		return "test"
	case commandLint:
		return "lint"
	case commandInstrument:
		return "instrument"
	case commandClean:
		return "clean"
	case commandWatch:
		return "watch"
	case commandDoctor:
		return "doctor"
	case commandTasks:
		return "tasks"
	case commandCache:
		return "cache"
	case commandVersion:
		return "version"
	case commandHelp:
		return "help"
	case commandImport:
		return "import"
	default:
		return "unknown"
	}
}

func commandNames() []string {
	return []string{
		"build", "cache", "clean", "doctor", "help", "import", "instrument",
		"lint", "run", "tasks", "test", "version", "watch",
	}
}

func taskSelection(command commandKind) (func(local string) bool, bool) {
	switch command {
	case commandBuild:
		return func(local string) bool {
			return local != "run" && !isTaskFamily(local, "lint") &&
				!isTaskFamily(local, "instrument") && !isTaskFamily(local, "instrument-apk")
		}, true
	case commandRun:
		return func(local string) bool { return local == "run" }, true
	case commandTest:
		return func(local string) bool { return isTaskFamily(local, "test") }, true
	case commandLint:
		return func(local string) bool { return isTaskFamily(local, "lint") }, true
	case commandInstrument:
		return func(local string) bool { return isTaskFamily(local, "instrument") }, true
	default:
		return nil, false
	}
}

func isTaskFamily(local, name string) bool {
	return local == name || strings.HasPrefix(local, name+"-")
}

func localTaskID(id string) string {
	if index := strings.LastIndex(id, ":"); index >= 0 {
		return id[index+1:]
	}
	return id
}

func selectTasks(tasks []*core.Task, project string, keep func(local string) bool) ([]*core.Task, error) {
	index := make(map[string]*core.Task, len(tasks))
	for _, task := range tasks {
		if task == nil {
			return nil, fmt.Errorf("task is nil")
		}
		if _, ok := index[task.ID]; ok {
			return nil, fmt.Errorf("duplicate task id %q", task.ID)
		}
		index[task.ID] = task
	}

	selected := make(map[string]struct{})
	var visit func(id string) error
	visit = func(id string) error {
		if _, ok := selected[id]; ok {
			return nil
		}
		task, ok := index[id]
		if !ok {
			return fmt.Errorf("task %q not found", id)
		}
		selected[id] = struct{}{}
		for _, dependency := range task.Deps {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		return nil
	}

	for _, task := range tasks {
		if project != "" && task.Project != project {
			continue
		}
		if keep(localTaskID(task.ID)) {
			if err := visit(task.ID); err != nil {
				return nil, err
			}
		}
	}

	result := make([]*core.Task, 0, len(selected))
	for _, task := range tasks {
		if _, ok := selected[task.ID]; ok {
			result = append(result, task)
		}
	}
	return result, nil
}
