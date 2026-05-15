package main

// Question represents a single interview question.
type Question struct {
	ID            string
	Text          string
	Options       []string // nil = free text input
	MaxSelections int      // 0 or 1 = single select, >1 = multiselect
}

var FixedQuestions = []Question{
	{
		ID:      "fixed_0",
		Text:    "Tabs or spaces?",
		Options: []string{"Tabs", "Spaces", "Whatever the linter says"},
	},
	{
		ID:      "fixed_1",
		Text:    "What's your go-to language right now?",
		Options: nil,
	},
	{
		ID:      "fixed_2",
		Text:    "Would you deploy on a Friday?",
		Options: []string{"Absolutely, YOLO", "Only if tests pass", "I have a family, so no"},
	},
	{
		ID:      "fixed_3",
		Text:    "Monolith or microservices?",
		Options: []string{"Monolith, always", "Microservices, obviously", "Whatever ships the feature"},
	},
	{
		ID:      "fixed_4",
		Text:    "Your git commit style?",
		Options: []string{"fix stuff", "Complete descriptive sentences", "Conventional commits (feat: ...)"},
	},
	{
		ID:      "fixed_5",
		Text:    "Which AI coding assistant do you prefer?",
		Options: []string{"Gemini", "Claude", "CoPilot", "None - I'm old school"},
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
	},
	{
		ID:      "extra_1",
		Text:    "What type of projects do you most enjoy working on?",
		Options: []string{"Web Development", "Mobile Development", "Backend Services", "Frontend Development", "Data Science/Engineering", "DevOps/Infrastructure", "Embedded Systems", "Game Development"},
	},
	{
		ID:      "extra_2",
		Text:    "What's your preferred development environment?",
		Options: []string{"IDE", "VIM", "Emacs", "Cloud", "Other"},
	},
	{
		ID:      "extra_3",
		Text:    "What's the weirdest bug you've ever fixed?",
		Options: nil,
	},
	{
		ID:      "extra_4",
		Text:    "Keyboard preference?",
		Options: []string{"Mechanical", "Laptop", "Wireless", "Ergonomic", "Other"},
	},
}
