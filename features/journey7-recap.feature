# Journey 7 of the route census: the recap.
Feature: The recap
  At the close the evening is recapped: the public page opens for the
  room, and every participant keeps a personal recap behind their
  recap link.

  Scenario: The public recap opens at the close
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    And the host has advanced the event to the Social
    And Ada and Grace have confirmed their meeting
    And Ada has shared their email in Settings
    When a visitor opens the public event recap
    Then the public recap is not available yet
    When the host closes the event
    Then the public event recap opens
    And the recap names the evening's personas and connections
    And the recap keeps identities and contact details to itself

  Scenario: A participant keeps their personal recap
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    And the host has advanced the event to the Social
    And Ada and Grace have confirmed their meeting
    And Ada has shared their email in Settings
    When the host closes the event
    Then Ada's personal page offers their recap link
    When Ada opens their recap
    Then Ada's recap shows their persona, their match and their connection
    And Ada's recap shows the contact details they shared
