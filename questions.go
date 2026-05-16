package main

// SelectionMode indicates whether a question allows single or multiple selections.
type SelectionMode int

const (
	// SingleSelect means the user can select only one option.
	SingleSelect SelectionMode = iota
	// MultiSelect means the user can select multiple options.
	MultiSelect
)

// Question represents a single interview question.
type Question struct {
	ID            string
	Text          string
	Options       []string // nil = free text input
	MaxSelections int      // 0 or 1 = single select, >1 = multiselect
	Mode          SelectionMode // SingleSelect or MultiSelect
}

var FixedQuestions = []Question{
	{
		ID:      "fixed_0",
		Text:    "Tabs or spaces?",
		Options: []string{"Tabs", "Spaces", "Whatever the linter says"},
		Mode:    SingleSelect,
	},
	{
		ID:      "fixed_1",
		Text:    "What's your go-to language right now?",
		Options: nil,
		Mode:    SingleSelect,
	},
	{
		ID:      "fixed_2",
		Text:    "Would you deploy on a Friday?",
		Options: []string{"Absolutely, YOLO", "Only if tests pass", "I have a family, so no"},
		Mode:    SingleSelect,
	},
	{
		ID:      "fixed_3",
		Text:    "Monolith or microservices?",
		Options: []string{"Monolith, always", "Microservices, obviously", "Whatever ships the feature"},
		Mode:    SingleSelect,
	},
	{
		ID:      "fixed_4",
		Text:    "Your git commit style?",
		Options: []string{"fix stuff", "Complete descriptive sentences", "Conventional commits (feat: ...)"},
		Mode:    SingleSelect,
	},
	{
		ID:      "fixed_5",
		Text:    "Which AI coding assistant do you prefer?",
		Options: []string{"Gemini", "Claude", "CoPilot", "None - I'm old school"},
		Mode:    SingleSelect,
	},
}

var TotalFixedQuestions = len(FixedQuestions)

const TotalCustomQuestions = 3

var TotalQuestions = TotalFixedQuestions + TotalCustomQuestions

var ExtraQuestions = []Question{
	{
		ID:            "extra_0",
		Text:          "What are your top 3 programming languages?",
		Options:       []string{"Go", "Python", "JavaScript", "TypeScript", "Java", "C#", "C++", "PHP", "Rust", "Swift"},
		MaxSelections: 3,
		Mode:          MultiSelect,
	},
	{
		ID:      "extra_1",
		Text:    "What type of projects do you most enjoy working on?",
		Options: []string{"Web Development", "Mobile Development", "Backend Services", "Frontend Development", "Data Science/Engineering", "DevOps/Infrastructure", "Embedded Systems", "Game Development"},
		Mode:    SingleSelect,
	},
	{
		ID:      "extra_2",
		Text:    "What's your preferred development environment?",
		Options: []string{"IDE", "VIM", "Emacs", "Cloud", "Other"},
		Mode:    SingleSelect,
	},
	{
		ID:      "extra_3",
		Text:    "What's the weirdest bug you've ever fixed?",
		Options: nil,
		Mode:    SingleSelect,
	},
	{
		ID:      "extra_4",
		Text:    "Keyboard preference?",
		Options: []string{"Mechanical", "Laptop", "Wireless", "Ergonomic", "Other"},
		Mode:    SingleSelect,
	},
}
