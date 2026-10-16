-- Acceptance is first attached to the OTP request, then linked to the account
-- only after its owner confirms the email. Deleting the account removes it.
ALTER TABLE login_codes ADD COLUMN legal_acceptance jsonb;

CREATE TABLE user_legal_acceptances (
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 consent_version text NOT NULL,
 rules_version text NOT NULL,
 consent_text text NOT NULL,
 rules_text text NOT NULL,
 accepted_at timestamptz NOT NULL,
 confirmed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,consent_version,rules_version)
);
