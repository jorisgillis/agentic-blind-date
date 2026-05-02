package main

type Question struct {
	ID      string
	Text    string
	Options []string // nil = free text input
}

var FixedQuestions = []Question{
	{
		ID:      "0",
		Text:    "Tabs or spaces?",
		Options: []string{"Tabs", "Spaces", "Whatever the linter says"},
	},
	{
		ID:      "1",
		Text:    "What's your go-to language right now?",
		Options: nil,
	},
	{
		ID:      "2",
		Text:    "Would you deploy on a Friday?",
		Options: []string{"Absolutely, YOLO", "Only if tests pass", "I have a family, so no"},
	},
	{
		ID:      "3",
		Text:    "Monolith or microservices?",
		Options: []string{"Monolith, always", "Microservices, obviously", "Whatever ships the feature"},
	},
	{
		ID:      "4",
		Text:    "Your git commit style?",
		Options: []string{"fix stuff", "Complete descriptive sentences", "Conventional commits (feat: ...)"},
	},
	{
		ID:      "5",
		Text:    "Which AI coding assistant do you prefer?",
		Options: []string{"Gemini", "Claude", "CoPilot", "None - I'm old school"},
	},
}

var TotalFixedQuestions = len(FixedQuestions)
const TotalCustomQuestions = 3
var TotalQuestions = TotalFixedQuestions + TotalCustomQuestions
