package packs

// androidChecklist lists the items of the legacy checklists with the group,
// severity and kind of checklist they had. A severity with a slash is the
// pair of a rule that needs judgement: the catalog keeps the second.
var androidChecklist = []checklistItem{
	{"Coroutines and concurrency", "global-scope", "BLOCKER", ""},
	{"Coroutines and concurrency", "async-task", "MAJOR", ""},
	{"Coroutines and concurrency", "run-blocking-prod", "BLOCKER", ""},
	{"Coroutines and concurrency", "hardcoded-dispatcher", "MAJOR", ""},
	{"Coroutines and concurrency", "blocking-on-main", "BLOCKER", ""},
	{"Coroutines and concurrency", "unstructured-launch", "MAJOR", ""},
	{"Coroutines and concurrency", "async-without-concurrency", "MINOR", ""},
	{"Coroutines and concurrency", "flow-error-unhandled", "MAJOR", ""},
	{"Coroutines and concurrency", "missing-flow-on", "MINOR", ""},
	{"State exposure", "exposed-mutable-flow", "MAJOR", ""},
	{"State exposure", "mutable-collection-exposed", "MAJOR", ""},
	{"State exposure", "state-not-modelled", "MAJOR", ""},
	{"State exposure", "events-in-state", "MAJOR", ""},
	{"State exposure", "new-live-data", "MAJOR", ""},
	{"State exposure", "local-broadcast-manager", "MAJOR", ""},
	{"Error handling", "empty-catch", "BLOCKER", ""},
	{"Error handling", "generic-exception", "MAJOR", ""},
	{"Error handling", "swallowed-error", "MAJOR", ""},
	{"Error handling", "no-double-bang", "BLOCKER/MAJOR", ""},
	{"Error handling", "manual-throw-precondition", "MINOR", ""},
}
