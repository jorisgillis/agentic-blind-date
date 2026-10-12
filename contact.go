package main

import (
	"database/sql"
)

// ContactChannel is one of a Participant's contact channels: a value they
// entered once in their personal page's settings, off by default. The github
// channel is pre-filled from their GitHub profile.
type ContactChannel struct {
	Channel string
	Label   string
	Value   string
	Enabled bool
}

// ContactChannelNames lists the contact channels in the settings' order.
var ContactChannelNames = []string{"github", "email", "linkedin", "instagram", "mastodon", "other"}

// channelLabels names each channel for the settings, the exchange prompt and
// the recap.
var channelLabels = map[string]string{
	"github":    "GitHub",
	"email":     "Email",
	"linkedin":  "LinkedIn",
	"instagram": "Instagram",
	"mastodon":  "Mastodon",
	"other":     "Other",
}

// ContactChannels owns the contact_channels table: the contact details a
// Participant shares with the people they met. Channels are off by default,
// and the mutual rule — Revealed — is the only way a channel's value leaves
// its owner's hands.
type ContactChannels struct {
	db       *DB
	meetings *Meetings
}

// NewContactChannels creates the ContactChannels module.
func NewContactChannels(db *DB) *ContactChannels {
	return &ContactChannels{db: db, meetings: NewMeetings(db, NewEventStore(db))}
}

// Channels lists the Participant's channels in the settings' order; channels
// never entered come back off with an empty value.
func (c *ContactChannels) Channels(id string) ([]ContactChannel, error) {
	rows, err := c.db.db.Query(`SELECT channel, value, enabled FROM contact_channels WHERE participant_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stored := map[string]ContactChannel{}
	for rows.Next() {
		var ch ContactChannel
		if err := rows.Scan(&ch.Channel, &ch.Value, &ch.Enabled); err != nil {
			return nil, err
		}
		stored[ch.Channel] = ch
	}
	out := make([]ContactChannel, 0, len(ContactChannelNames))
	for _, name := range ContactChannelNames {
		ch := stored[name] // the zero value when never entered: off, empty
		ch.Channel = name
		ch.Label = channelLabels[name]
		out = append(out, ch)
	}
	return out, rows.Err()
}

// Save records the Participant's channels — entered once in their settings,
// an upsert per channel, all in one transaction. The settings form always
// saves every channel, so a save rewrites them all; a channel the caller
// does not carry keeps its stored row.
func (c *ContactChannels) Save(id string, channels []ContactChannel) error {
	return c.db.inTx(func(tx *sql.Tx) error {
		for _, ch := range channels {
			if _, err := tx.Exec(
				`INSERT OR REPLACE INTO contact_channels (participant_id, channel, value, enabled) VALUES (?, ?, ?, ?)`,
				id, ch.Channel, ch.Value, ch.Enabled,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// Revealed returns the channels owner shares with viewer — the mutual rule,
// in the one place it lives: a channel is revealed to the other side only if
// the owner opted in to it AND the pair's meeting is completed. No contact
// data is ever visible without the completed meeting.
func (c *ContactChannels) Revealed(owner, viewer string) ([]ContactChannel, error) {
	m, err := c.meetings.Between(owner, viewer)
	if err != nil {
		return nil, err
	}
	if m == nil || !m.Completed() {
		return nil, nil
	}
	channels, err := c.Channels(owner)
	if err != nil {
		return nil, err
	}
	return shared(channels), nil
}

// shared filters a channel list to what it shares: an opted-in channel with
// a value — a channel with no value shares nothing.
func shared(channels []ContactChannel) []ContactChannel {
	var out []ContactChannel
	for _, ch := range channels {
		if ch.Enabled && ch.Value != "" {
			out = append(out, ch)
		}
	}
	return out
}

// ChannelCount is one Participant's opted-in channel count — the debug
// endpoint's counts-only view of the contact channels.
type ChannelCount struct {
	ParticipantID string `json:"participant_id"`
	PersonaName   string `json:"persona_name"`
	Enabled       int    `json:"enabled"`
}

// Counts reports, per Participant, how many channels they opted in to — the
// /data debug endpoint shows counts only, never values.
func (c *ContactChannels) Counts() ([]ChannelCount, error) {
	rows, err := c.db.db.Query(`
		SELECT p.id, p.persona_name, COUNT(cc.channel) AS enabled
		FROM participants p
		LEFT JOIN contact_channels cc ON cc.participant_id = p.id AND cc.enabled = 1
		GROUP BY p.id
		ORDER BY enabled DESC, p.persona_name, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChannelCount
	for rows.Next() {
		var row ChannelCount
		if err := rows.Scan(&row.ParticipantID, &row.PersonaName, &row.Enabled); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// RemoveParticipant deletes a Participant's channels when they leave the
// event.
func (c *ContactChannels) RemoveParticipant(id string) error {
	_, err := c.db.db.Exec(`DELETE FROM contact_channels WHERE participant_id = ?`, id)
	if err == nil {
		c.db.changed()
	}
	return err
}
