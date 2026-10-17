# The steps are declarative CONTEXT.md domain language: no routes, no form
# fields, no selectors — the bindings own all of that. Slide Deck, Host,
# Participant.
Feature: The host shares the speakers' slide decks
  A Slide Deck is a meet-up speaker's PDF the Host uploads; every
  Participant sees the decks on their personal page and can open or
  download them.

  Scenario: The host uploads a speaker's slides
    Given a fresh event
    And Ada has joined and answered their interview
    When the host uploads a slide deck titled "Gophers in Production" for the speaker "Roberta Pike"
    Then the host's slides page lists "Gophers in Production"
    And Ada's personal page offers the slides "Gophers in Production" by "Roberta Pike"

  Scenario: A participant opens and downloads the slides
    Given a fresh event
    And Ada has joined and answered their interview
    And the host has uploaded a slide deck titled "Gophers in Production" for the speaker "Roberta Pike"
    When Ada opens the slide deck
    Then Ada's browser shows the slides as a PDF
    When Ada downloads the slide deck
    Then the download arrives as an attachment

  Scenario: The host takes the slides down
    Given a fresh event
    And Ada has joined and answered their interview
    And the host has uploaded a slide deck titled "Gophers in Production" for the speaker "Roberta Pike"
    When the host removes the slide deck
    Then the host's slides page lists no slide decks
    And Ada's personal page offers no slide decks
