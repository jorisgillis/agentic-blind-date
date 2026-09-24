package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
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
	if p.Profile != nil {
		if summary := p.Profile.Summary(); summary != "" {
			parts = append(parts, summary)
		}
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
		return Persona{Name: "The " + strings.ToUpper(lang[:1]) + lang[1:] + " Developer", Tagline: "Ships things."}
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
