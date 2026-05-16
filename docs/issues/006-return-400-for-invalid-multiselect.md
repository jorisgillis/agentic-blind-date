# Issue: Return 400 Bad Request for invalid multi-select submissions

## Description
When multi-select validation fails, return HTTP 400 Bad Request with an appropriate error message.

## Details
- Return 400 status code
- Include error message: "Too many selections. Maximum X allowed."
- Don't proceed with answer storage

## Acceptance Criteria
- [ ] Invalid submissions return 400 Bad Request
- [ ] Error message is clear and user-friendly
- [ ] Valid submissions continue to work normally

## Priority
High

## Labels
`enhancement, backend, validation`

## Type
Feature
