# config.toml

English · [Español](config.es.md) · [Deutsch](config.de.md)

Every key Thaler reads from its configuration file: what it does, what it takes and what it is without it. [`config.example.toml`](../config.example.toml) is the same in the form of a file to start from.

Thaler runs without a config file; everything below has a default. The file is `~/.config/thaler/config.toml` (or `$XDG_CONFIG_HOME/thaler/config.toml`), or the file given with `--config`, which must then exist. In the interface, `C` opens it in your editor, creating it from the example with every line switched off if there is none; `ctrl+l` and `ctrl+t` write `language` and `theme` for you, and the rules screen and the rule hint (`tab` in the category picker) change the `[[rule]]` tables in place, leaving your comments and everything else as you wrote them.

**The file is read strictly.** A key Thaler does not know is an error, so a typo cannot silently switch a rule or a setting off; keys are lower case, so `Language` or `[[Rule]]` is a typo too. A value out of range is an error that names the key. A file with an error is not used: `thaler cli` commands that need it stop with the error, and the interface says what is wrong and refuses to import until it is fixed. It is read again for every import, so a change applies to the next one.

The keys below are written in full: `profile.columns.date` is `date` under `[profile.columns]` of a `[[profile]]`.

## Settings

| Key | Takes | Without it | What it does |
| --- | --- | --- | --- |
| `language` | `"en"`, `"es"` or `"de"` | the environment decides: `LC_ALL`, `LC_MESSAGES`, then `LANG` (`de_DE.UTF-8`); a language Thaler does not speak is English | The language of the interface. A message not yet translated is shown in English. `thaler cli` is English whatever this says. `"pseudo"` is a made-up language, longer and accented, for finding text that does not fit. |
| `theme` | `"auto"`, `"dark"` or `"light"` | `"auto"` | The colours of the interface. `"auto"` follows the terminal's background; `"dark"` and `"light"` fix them for a terminal that does not say what its background is (some multiplexers, a session over ssh). |
| `import_dir` | a folder; `~` is your home | `~/Downloads` | The folder the interface lists bank exports from when you press `i`. You can always type or drop another file or folder. |
| `watch_import_dir` | `true` or `false` | `true` | Whether the interface looks at `import_dir` every few seconds while it runs, only at the names and times of its CSV files, and says in the bottom bar when a new export has arrived. |
| `backup_reminder_days` | a whole number, 0 or more | `7` | The interface reminds you to back up when the newest backup is this many days old, or there is none and you have transactions. `0` switches the reminder off. |
| `keep_backups` | a whole number, 0 or more | `10` | How many of the newest backups to keep in each backups folder: the backups you make, the databases restores replaced and the copies made before upgrades. When there are more, the interface offers to remove the oldest (`ctrl+p`) and `thaler cli prune` lists them; nothing is removed without a yes. `0` keeps everything and stops the offer. |

## Features

| Key | Takes | Without it | What it does |
| --- | --- | --- | --- |
| `features.<name>` | `true` or `false` | off | Switches on a story that is merged before it is finished, to try it early. A name that does not exist is refused, and the error lists the ones that do. Nothing is behind a flag in this version. |

```toml
[features]
some_feature = true
```

## Rules: `[[rule]]`

Each `[[rule]]` gives imported transactions a category. They are checked from the top, and the first that matches wins; a transaction no rule matches is left in `Expenses:Uncategorized` or `Income:Uncategorized` for you to decide. A rule matches the payee, the purpose and the booking text of a transaction, joined by spaces, ignoring case.

| Key | Takes | Without it | What it does |
| --- | --- | --- | --- |
| `rule.contains` | text | — | Matches a transaction whose text contains this, ignoring case and the spaces around it. |
| `rule.regex` | a regular expression ([Go syntax](https://pkg.go.dev/regexp/syntax)) | — | Matches a transaction whose text this finds, ignoring case. |
| `rule.category` | an account name, such as `Expenses:Groceries` | required | The category a matching transaction is booked to. One that does not exist yet is created on import. `Equity:Transfers` marks money moved between your own accounts; Thaler pairs the two halves of such a transfer by itself (same amount, within three days), and a rule only marks one whose other half is not imported yet. |

A rule has exactly one of `contains` and `regex`, and a `category`; a rule with neither, both, a broken expression or a category that is not a valid name is an error naming the rule's number.

```toml
[[rule]]
contains = "REWE"
category = "Expenses:Groceries"

[[rule]]
regex = "b(ä|ae)ckerei|edeka|lidl|aldi"
category = "Expenses:Groceries"
```

## Bank profiles: `[[profile]]`

A profile says how to read one bank's export. Thaler has three built in: `sparkasse` (the CSV-CAMT and CSV-MT940 exports of Sparkasse online banking), `ofx` and `qif`. A profile of yours is needed only for another bank's CSV, or for a QIF file whose dates a built-in profile reads the wrong way round.

`thaler cli import --profile NAME` chooses a profile; without it, and always in the interface, it is found from the file (`auto`): an OFX or QIF file by what it starts with, a CSV file by its header. Of the CSV profiles whose date and amount columns the header has, the one that finds the most of its columns wins, and of equals yours come first, in the order written. A profile of yours with the name of a built-in one replaces it.

| Key | Takes | Without it | What it does |
| --- | --- | --- | --- |
| `profile.name` | a name | required | What `--profile` calls it, ignoring case, and what an import reports it read the file with. |
| `profile.format` | `"csv"`, `"ofx"` or `"qif"` | `"csv"` | The kind of file. An OFX file needs nothing else; a QIF file takes `date_formats` and `decimal_separator`; the columns, the delimiter, the encoding and the skip rule are for CSV alone. |
| `profile.delimiter` | one character, or `"auto"` | `"auto"` | What separates the columns. `"auto"` tries `;`, `,`, tab and `\|` and keeps the one that splits the header into the date and amount columns. |
| `profile.encoding` | `"auto"`, `"utf-8"`, `"utf-16"`, `"utf-16le"`, `"utf-16be"` or `"windows-1252"` (also `"latin1"`, `"iso-8859-1"`) | `"auto"` | The character encoding. `"auto"` reads UTF-16 or UTF-8 when the bytes say so, and Windows-1252 otherwise, which German banks still use. |
| `profile.decimal_separator` | `","` or `"."` | worked out for each amount | The decimal separator of the amounts. Set it when amounts such as `1.234` could be read either way. |
| `profile.date_formats` | a list of formats made of `DD`, `MM`, `YY` and `YYYY`, such as `"DD.MM.YYYY"` | `["YYYY-MM-DD", "DD.MM.YYYY"]` | How dates are written. Each date is read with the first format that fits; a QIF file uses the first format that reads every date in it. A Go layout such as `"2006-01-02"` also works. |
| `profile.skip_column` | a column name | no row is skipped | With `skip_contains`: a row whose value in this column contains `skip_contains`, ignoring case, is left out, such as a pending booking. |
| `profile.skip_contains` | text | no row is skipped | See `skip_column`. |

### Columns: `[profile.columns]`

Each key lists the header names that may hold that field; the first one the file has is used. Header names match ignoring case, and `ä`, `ö`, `ü` and `ß` count as `ae`, `oe`, `ue` and `ss`. A CSV profile needs `date` and `amount`; the others are left empty when not given.

| Key | Takes | Without it | What it does |
| --- | --- | --- | --- |
| `profile.columns.date` | a list of header names | required for CSV | The booking date. |
| `profile.columns.amount` | a list of header names | required for CSV | The amount, negative for money going out. |
| `profile.columns.payee` | a list of header names | the booking text | Who was paid or who paid. |
| `profile.columns.purpose` | a list of header names | empty | The purpose or reference. |
| `profile.columns.iban` | a list of header names | empty | The other side's account. |
| `profile.columns.booking_text` | a list of header names | empty | The kind of booking (card payment, transfer, fee). |
| `profile.columns.currency` | a list of header names | the account's currency | The currency of each row. |

The payee, the purpose and the booking text are what rules match. With the date, the amount and the IBAN they also make up the key that recognises a row imported before, so changing which columns a profile reads can make an export import again.

A profile has a problem when its format, delimiter, encoding or decimal separator is none of the values above, or a CSV profile lacks the date or amount column; the error names the profile.

```toml
[[profile]]
name = "mybank"
delimiter = ","
decimal_separator = "."
date_formats = ["YYYY-MM-DD"]
skip_column = "Status"
skip_contains = "pending"

[profile.columns]
date = ["Date", "Booking date"]
payee = ["Description"]
amount = ["Amount"]

[[profile]]
name = "mybank-qif"
format = "qif"
date_formats = ["DD/MM/YY"]
decimal_separator = ","
```
