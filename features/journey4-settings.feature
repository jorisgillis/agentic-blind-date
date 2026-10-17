# Journey 4 of the route census (ADR-0022): Settings and theming. The
# steps stay in CONTEXT.md's domain language — Settings, Contact
# Channel, Theme, Public Theme; the bindings own every route and form.
Feature: Settings and theming
  A participant tunes their personal pages in Settings: the Theme that
  paints them, and the contact channels they may share once a meeting
  is completed. The host picks the Public theme for the public
  surfaces.

  Scenario: A participant picks a theme for their personal pages
    Given a fresh event
    And Ada has joined and answered their interview
    When Ada picks the Daylight theme in Settings
    Then Ada's personal page is painted in Daylight
    And the landing page stays in Midnight

  Scenario: The host picks the Public theme
    Given a fresh event
    When the host picks Daylight as the Public theme
    Then the landing page is painted in Daylight
    And the big screen is painted in Daylight

  Scenario: A shared contact channel reaches a completed pair
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    And the host has advanced the event to the Social
    And Ada and Grace have confirmed their meeting
    When Ada shares their email in Settings
    Then Ada's settings show the email ready to share
    And Grace's exchange prompt shows Ada's email
