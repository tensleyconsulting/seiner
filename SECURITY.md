# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately through the repository's **Security** tab, with
**Report a vulnerability**. Include the Seiner version (`seiner --version`),
your OS and terminal, and the steps to reproduce it. Leave out real project
IDs, account emails, and credentials; `seiner --demo` reproduces most UI
problems without them.

## Supported versions

Fixes go into the latest release only.

## What Seiner touches

Knowing this helps judge a report:

- Seiner reads Google Cloud APIs with your Application Default Credentials,
  optionally impersonating a service account. It has no code path that
  changes or deletes a cloud resource.
- It never reads secret payloads, service account key material, or table
  data.
- Its log file holds account emails and Console URLs. It is written with
  mode 0600 under `$XDG_STATE_HOME/seiner`, and a symlink there is refused.
- Copy uses the terminal's OSC 52 clipboard sequence.
- `--icons-dir` reads SVG and PNG files from a directory you name.
