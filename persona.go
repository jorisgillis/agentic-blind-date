package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Persona is a Participant's fun, anonymous identity in the room.
type Persona struct {
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
}

// Personas is the Persona module: it creates a Participant's Persona through
// the LLM and falls back to an anonymous one when the LLM fails. A Persona
// never reveals who the Participant is.
type Personas struct {
	llm LLM
}

// NewPersonas creates the Persona module.
func NewPersonas(llm LLM) *Personas {
	return &Personas{llm: llm}
}

// Create makes a Persona from the Participant's profile and interview answers.
func (ps *Personas) Create(p *Participant) Persona {
	persona, err := ps.generate(p)
	if err != nil {
		log.Printf("Persona generation error for %s: %v", p.ID, err)
		return fallbackPersona(p)
	}
	return persona
}

func (ps *Personas) generate(p *Participant) (Persona, error) {
	system := `You are a fun tech personality generator for a programming meetup blind date event.
Create a funny, tongue-in-cheek anonymous persona based on a developer's profile and interview answers.
Respond with ONLY a valid JSON object — no markdown, no backticks:
{"name": "The [Adjective] [Tech Noun]", "tagline": "<funny one-liner max 60 chars>"}`

	response, err := ps.llm.Chat(system, personaPrompt(p))
	if err != nil {
		return Persona{}, err
	}
	var persona Persona
	if err := json.Unmarshal([]byte(extractJSON(response)), &persona); err != nil {
		return Persona{}, fmt.Errorf("persona parse error: %v (raw: %s)", err, response)
	}
	if persona.Name == "" {
		return Persona{}, fmt.Errorf("persona reply has no name (raw: %s)", response)
	}
	return persona, nil
}

func personaPrompt(p *Participant) string {
	var parts []string
	if summary := p.Summary(); summary != "" {
		parts = append(parts, summary)
	}
	parts = append(parts, "\nInterview answers:")
	ids := make([]string, 0, len(p.Answers))
	for id := range p.Answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("Q[%s]: %s", id, p.Answers[id]))
	}
	return strings.Join(parts, "\n")
}

// fallbackPersona names the Participant after their main language, never after
// their handle or name.
func fallbackPersona(p *Participant) Persona {
	if lang := mainLanguage(p); lang != "" {
		first, size := utf8.DecodeRuneInString(lang)
		return Persona{Name: "The " + string(unicode.ToUpper(first)) + lang[size:] + " Developer", Tagline: "Ships things."}
	}
	return Persona{Name: "The Mysterious Coder", Tagline: "Ships things."}
}

func mainLanguage(p *Participant) string {
	if p.Profile != nil {
		if len(p.Profile.Languages) > 0 {
			return p.Profile.Languages[0]
		}
		if ea := p.Profile.ExtraAnswers; ea != nil && len(ea.Languages) > 0 {
			return ea.Languages[0]
		}
	}
	return strings.TrimSpace(p.Answers["fixed_1"])
}

// paletteColor is one Persona colour, with its text colour and hex for every display.
type paletteColor struct {
	Name  string // what the Participant is told their colour is
	Class string // Tailwind background class stored on the Participant
	Text  string // readable Tailwind text class on that background
	Hex   string // for the Big Screen graph
}

// personaPalette is the one source of Persona colours.
var personaPalette = []paletteColor{
	{"teal", "bg-teal-400", "text-teal-900", "#2dd4bf"},
	{"red", "bg-red-400", "text-red-900", "#f87171"},
	{"purple", "bg-purple-400", "text-purple-900", "#c084fc"},
	{"amber", "bg-amber-400", "text-amber-900", "#fbbf24"},
	{"blue", "bg-blue-400", "text-blue-900", "#60a5fa"},
}

var personaSymbols = []string{"🦊", "🦁", "🐯", "🐺", "🦝", "🦔", "🐙", "🦈", "🦅", "🐸"}

// nextLook picks a Persona colour and symbol given how often each combination
// ("class|symbol") is already used: a random one among the least used, so every
// combination is used once before any repeats, and repeats then cycle.
func nextLook(uses map[string]int) (color, symbol string) {
	var least [][2]string
	fewest := -1
	for _, c := range personaPalette {
		for _, s := range personaSymbols {
			n := uses[c.Class+"|"+s]
			switch {
			case fewest == -1 || n < fewest:
				fewest, least = n, [][2]string{{c.Class, s}}
			case n == fewest:
				least = append(least, [2]string{c.Class, s})
			}
		}
	}
	pick := least[rand.Intn(len(least))]
	return pick[0], pick[1]
}

// paletteHex maps each Persona colour class to its hex, for the Big Screen.
func paletteHex() map[string]string {
	m := make(map[string]string, len(personaPalette))
	for _, c := range personaPalette {
		m[c.Class] = c.Hex
	}
	return m
}

// paletteColorOf returns the palette entry for a Persona colour class; unknown
// classes get a neutral grey.
func paletteColorOf(class string) paletteColor {
	for _, c := range personaPalette {
		if c.Class == class {
			return c
		}
	}
	return paletteColor{Name: "grey", Class: class, Text: "text-gray-900", Hex: "#6b7280"}
}

// paletteText returns the readable text class for a Persona colour class.
func paletteText(class string) string { return paletteColorOf(class).Text }

// paletteName returns the name of a Persona colour class, such as "teal".
func paletteName(class string) string { return paletteColorOf(class).Name }
