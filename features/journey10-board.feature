# Board and Post follow CONTEXT.md: a Post is visible to nobody but its
# poster until the Host approves it; personas only, everywhere.
Feature: The board carries the meet-up's shared posts
  A Participant posts a short message on the Board; it reaches the
  room only once the Host approves it, and the Host can take it down.

  Scenario: A post waits for the host's approval
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    When Ada posts "Gopher facts, hot off the press" on the board
    Then Ada's board shows the post "Gopher facts, hot off the press" waiting for the host
    And Grace's board shows no posts

  Scenario: The host approves the post
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada has posted "Gopher facts, hot off the press" on the board
    When the host approves Ada's post
    Then Grace's board shows the post "Gopher facts, hot off the press" under Ada's persona
    And the big screen rotates the post on its board card

  Scenario: The host takes an approved post down
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Ada has posted "Gopher facts, hot off the press" on the board
    And the host has approved Ada's post
    When the host takes Ada's post down
    Then Grace's board shows no posts

  Scenario: A participant deletes their own post
    Given a fresh event
    And Ada has joined and answered their interview
    And Ada has posted "Gopher facts, hot off the press" on the board
    When Ada deletes their post
    Then Ada's board shows no posts
