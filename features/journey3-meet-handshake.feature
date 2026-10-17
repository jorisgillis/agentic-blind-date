# Given steps are past tense (context that already happened), When steps
# present tense (the behavior under test).
Feature: Meeting a match with Meet Codes
  During the Social, two matched participants confirm a physical meeting
  with each other's Meet Codes.

  Scenario: The pair completes the two-sided handshake
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When the host advances the event to the Social
    And Ada confirms a meeting with Grace's meet code
    And Grace confirms a meeting with Ada's meet code
    Then Ada and Grace have a completed meeting
    And both Ada and Grace have one connection
