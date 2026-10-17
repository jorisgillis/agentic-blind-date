# The steps are declarative: no routes, no form fields, no selectors — the
# bindings own all of that. Domain language follows CONTEXT.md:
# Participant, Interview, Persona, Meet Code, Meeting.
Feature: Joining without a GitHub account
  A participant without a GitHub profile joins the event, answers their
  Interview, and receives a Persona.

  Scenario: A no-GitHub participant reaches Ready
    Given a fresh event
    When Ada joins without a GitHub account
    And Ada answers every interview question
    Then Ada has the persona "The Gopher"
    And Ada is ready for matching
    And Ada waits for a match
