# Data and privacy

- **Financial data never goes in the repo.** `.gitignore` excludes `*.db*`, `*.csv` (except the importer fixture) and `config.toml`. Do not paste real transactions, accounts or amounts into code, tests, docs or commit messages.
- For smoke tests use a throwaway database: `THALER_DB=<scratch>/t.db` or `--db`, and `--config <scratch>/cfg.toml`, in a scratch folder outside the repo. The current database is test data until the maintainer says real use has started.
- **Nothing destructive happens by itself.** Backups are plain files unless encrypted, a restore keeps the database it replaces (`backups/replaced/`), an upgrade copies the database first (`backups/pre-migration/`), and pruning only ever lists, then removes after a yes. New features that remove or overwrite data follow the same shape: check first, keep the old one, ask, then act.
