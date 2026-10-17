# Host, Phase, Connection, Champion and Host Export Pack follow
# CONTEXT.md. Rematching is an onboarding-only action; the Reset wipes
# the evening; the export pack carries personas and counts only.
Feature: The host runs the evening from the console
  The Host advances the evening, runs the rematch, resets the event,
  and downloads the post-meetup blast pack.

  Scenario: The console shows the evening
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    When the host opens the console
    Then the console shows the onboarding phase
    And the console counts two participants

  Scenario: The host runs a rematch during onboarding
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the host starts a rematch
    Then the console says the matchmaker agents are at work
    And the big screen's graph still shows Ada and Grace matched

  Scenario: The rematch is refused once matching is frozen
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the host advances the event to the Social
    And the host starts a rematch
    Then the console says matching is frozen, pairs are final
    And the big screen's graph still shows Ada and Grace matched

  Scenario: The host resets the event
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the host advances the event to the Social
    And Ada confirms a meeting with Grace's meet code
    And Grace confirms a meeting with Ada's meet code
    And the host resets the event
    Then the connection leaderboard is empty
    And the big screen shows the onboarding phase again

  Scenario: The host's export pack carries the evening
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the host advances the event to the Social
    And Ada confirms a meeting with Grace's meet code
    And Grace confirms a meeting with Ada's meet code
    And the host closes the event
    And the host downloads the export pack
    Then the pack names the champion with one connection
    And the pack carries the personas, never the identities
