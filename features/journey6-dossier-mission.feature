# Journey 6 of the route census: the dossier and the mission card.
Feature: The dossier and the mission card
  The Match's dossier stays locked until the pair's meeting completes,
  and the mission card goes live at the Social, stamping its tiles as
  meetings complete.

  Scenario: The dossier unlocks when the pair completes their meeting
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    And the matchmaker has written Ada and Grace's dossier
    When the host advances the event to the Social
    Then Ada's dossier is locked
    When Ada confirms a meeting with Grace's meet code
    And Grace confirms a meeting with Ada's meet code
    Then Ada's dossier is unlocked
    And Grace's dossier is unlocked

  Scenario: The mission card goes live at the Social
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Hedy has joined and answered their interview
    When the host advances the event to the Social
    Then Ada's mission card is live
    And Ada's mission card is on record

  Scenario: A completed meeting stamps the pair's tile
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    And the host has advanced the event to the Social
    And Ada's mission card is live
    When Ada confirms a meeting with Grace's meet code
    And Grace confirms a meeting with Ada's meet code
    Then Ada's mission card stamps Grace's tile
