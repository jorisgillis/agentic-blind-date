# Big Screen, Graph, Connection and Phase follow CONTEXT.md: personas
# only, in every phase; the leaderboard counts connections from the
# Social; everything arrives by push.
Feature: The big screen shows the evening to the room
  The Big Screen is the public display: the Graph of personas, the
  connection leaderboard once the Social opens, and every change
  arriving by push.

  Scenario: The graph shows every participant as their persona
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the big screen loads
    Then the big screen renders the graph
    And the graph names every participant as their persona, never their identity

  Scenario: The connection leaderboard counts the social's meetings
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the host advances the event to the Social
    And Ada confirms a meeting with Grace's meet code
    And Grace confirms a meeting with Ada's meet code
    Then the big screen's connection leaderboard shows Ada with one connection
    And the big screen's connection leaderboard shows Grace with one connection

  Scenario: The screen hears every change by push
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    And the big screen is watching for changes
    When the host advances the event to the Social
    Then the big screen's stream announces the Social
