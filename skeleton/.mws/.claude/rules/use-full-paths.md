# Always Use Full Paths

Never use `cd` to change directories in Bash commands. Directory changes don't persist between commands.

## Exception: project task runner

If the project ships a root-level task runner (Makefile, justfile, npm/pnpm scripts at the root), invoke those targets directly. They handle directory context internally:

```bash
just dev
make test
pnpm -w lint
```

## DO

```bash
# Full paths.
pytest backend/tests/
ls ./src/

# Subshells for multi-command sequences.
(cd frontend && pnpm install && pnpm build)
```

## DON'T

```bash
# Don't use standalone cd -- directory won't persist after this command.
cd frontend && pnpm install
cd src
```

## Why

Each Bash command runs in a fresh shell. Using `cd` wastes tokens and creates confusion about the current directory.
