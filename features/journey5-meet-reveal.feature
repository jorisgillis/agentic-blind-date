# Journey 5 of the route census: the meet page and the reveal.
Feature: The meet page and the reveal
  A meet link shows the persona behind a meet code, and the host's
  reveal at the break opens every match page.

  Scenario: A meet link shows the persona while the event onboards
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    When Grace opens Ada's meet link
    Then Grace sees Ada's persona card
    And meeting Ada waits for the Social

  Scenario: The host's reveal opens the match pages
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada and Grace are matched
    When Ada opens their match page
    Then Ada is sent back to the wait page
    When the host reveals the matches
    Then Ada's match page shows the match and its compatibility score
    And Grace's match page shows the match and its compatibility score
