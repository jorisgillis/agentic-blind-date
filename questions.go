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
}

const TotalFixedQuestions = 5
const TotalCustomQuestions = 3
const TotalQuestions = TotalFixedQuestions + TotalCustomQuestions
