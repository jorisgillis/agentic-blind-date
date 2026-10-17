# The debug data endpoints follow CONTEXT.md: the Dossier stays
# scrubbed until the pair's Meeting completes, the channel counts never
# carry values, and the Mission Card's data route belongs to journey 6.
Feature: The debug data behind the event
  The event's debug endpoints: the participants with their scrubbed
  dossiers, the meetings, the activity log, the channel counts, and
  the host's participant removal.

  Scenario: The debug index shows the evening
    Given a fresh event
    And Ada has joined and answered their interview
    When the debug index is opened
    Then it shows the event's phase
    And it lists the one participant

  Scenario: A locked dossier stays scrubbed until the pair met
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the debug data is read for Ada
    Then it shows Ada's persona
    And the match dossier stays scrubbed
    When the host advances the event to the Social
    And Ada confirms a meeting with Grace's meet code
    And Grace confirms a meeting with Ada's meet code
    Then the debug data for Ada carries the unlocked dossier

  Scenario: The meetings and the activity log
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the host advances the event to the Social
    And Ada confirms a meeting with Grace's meet code
    And Grace confirms a meeting with Ada's meet code
    When the debug meetings are read
    Then they show the completed meeting between Ada and Grace
    And the activity log records the meeting

  Scenario: The channel counts, never the values
    Given a fresh event
    And Ada has joined and answered their interview
    And Ada has opted in to their email channel
    When the debug channel counts are read
    Then they show Ada with one channel
    And they never show Ada's email address

  Scenario: The host removes a participant
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Bob has joined and answered their interview
    When the host removes Bob from the event
    Then Bob's debug record is gone
    And the debug data shows two participants
