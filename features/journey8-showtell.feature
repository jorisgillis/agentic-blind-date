# Journey 8 of the route census: show & tell — sign-up, the host queue,
# presenter mode and the audience questions.
Feature: Show and tell
  Participants sign up with a talk from the Social on, the host runs the
  queue in presenter mode from Show & Tell, and the audience asks the
  presenter questions through their personas.

  Scenario: A participant signs up with a talk
    Given a fresh event
    And Ada has joined and answered their interview
    And the host has advanced the event to the Social
    When Ada signs up with the talk "Gophers all the way down"
    Then Ada's show and tell section shows "Gophers all the way down" in the queue
    And the host's queue lists "Gophers all the way down"
    When Ada signs up with the talk "Shipping gophers"
    Then Ada's show and tell section shows "Shipping gophers" in the queue
    And the host's queue no longer lists "Gophers all the way down"

  Scenario: The host runs presenter mode and the audience asks
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And the host has advanced the event to the Social
    And Ada has signed up with the talk "Gophers all the way down"
    And Grace has signed up with the talk "Ship it on Friday"
    And the host has advanced the event to Show & Tell
    When the host puts Ada's talk on stage
    Then the host's queue shows Ada's talk on stage
    And Grace's question form is open for the presenter
    When Grace asks "But does it scale?"
    Then the host's question queue carries Grace's question
    When the host puts Grace's question on the big screen
    And the host marks Grace's question answered
    Then Grace's question is answered

  Scenario: The host shapes the queue
    Given a fresh event
    And Ada has joined and answered their interview
    And Grace has joined and answered their interview
    And Hedy has joined and answered their interview
    And the host has advanced the event to the Social
    And Ada has signed up with the talk "Gophers all the way down"
    And Grace has signed up with the talk "Ship it on Friday"
    And Hedy has signed up with the talk "Types are tests"
    When the host moves Hedy's talk up
    Then the host's queue shows Hedy's talk ahead of Grace's
    When the host removes Ada's talk
    Then the host's queue no longer lists "Gophers all the way down"
    And Ada's show and tell section shows the sign-up form again
