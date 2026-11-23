<!-- translated from docs/config.md at 0195b7a4892c -->
# config.toml

[English](config.md) · Español · [Deutsch](config.de.md)

Cada clave que Thaler lee de su archivo de configuración: qué hace, qué admite y qué vale sin ella. [`config.example.toml`](../config.example.toml) es lo mismo en forma de archivo del que partir.

Thaler funciona sin archivo de configuración; todo lo de abajo tiene un valor por defecto. El archivo es `~/.config/thaler/config.toml` (o `$XDG_CONFIG_HOME/thaler/config.toml`), o el que se indique con `--config`, que entonces tiene que existir. En la interfaz, `C` lo abre en tu editor, creándolo a partir del ejemplo con todas las líneas desactivadas si no hay ninguno; `ctrl+l` y `ctrl+t` escriben `language` y `theme` por ti, y la pantalla de reglas y la sugerencia de regla (`tab` en el selector de categorías) cambian las tablas `[[rule]]` en su sitio, dejando tus comentarios y todo lo demás como lo escribiste.

**El archivo se lee de forma estricta.** Una clave que Thaler no conoce es un error, así que una errata no puede desactivar sin avisar una regla o un ajuste; las claves van en minúsculas, así que `Language` o `[[Rule]]` también son una errata. Un valor fuera de rango es un error que nombra la clave. Un archivo con un error no se usa: los comandos de `thaler cli` que lo necesitan se detienen con el error, y la interfaz dice qué está mal y se niega a importar hasta que se corrija. Se vuelve a leer en cada importación, así que un cambio se aplica a la siguiente.

Las claves de abajo se escriben completas: `profile.columns.date` es `date` bajo `[profile.columns]` de un `[[profile]]`.

## Ajustes

| Clave | Admite | Sin ella | Qué hace |
| --- | --- | --- | --- |
| `language` | `"en"`, `"es"` o `"de"` | decide el entorno: `LC_ALL`, `LC_MESSAGES` y luego `LANG` (`de_DE.UTF-8`); un idioma que Thaler no habla es inglés | El idioma de la interfaz. Un mensaje aún sin traducir se muestra en inglés. `thaler cli` está en inglés diga lo que diga esto. `"pseudo"` es un idioma inventado, más largo y con acentos, para encontrar texto que no cabe. |
| `theme` | `"auto"`, `"dark"` o `"light"` | `"auto"` | Los colores de la interfaz. `"auto"` sigue el fondo del terminal; `"dark"` y `"light"` los fijan para un terminal que no dice cuál es su fondo (algunos multiplexores, una sesión por ssh). |
| `import_dir` | una carpeta; `~` es tu carpeta personal | `~/Downloads` | La carpeta de la que la interfaz lista los extractos del banco cuando pulsas `i`. Siempre puedes escribir o soltar otro archivo u otra carpeta. |
| `watch_import_dir` | `true` o `false` | `true` | Si la interfaz mira `import_dir` cada pocos segundos mientras está abierta, solo los nombres y las horas de sus archivos CSV, y dice en la barra inferior cuándo ha llegado un extracto nuevo. |
| `backup_reminder_days` | un número entero, 0 o más | `7` | La interfaz te recuerda hacer una copia de seguridad cuando la más reciente tiene este número de días, o no hay ninguna y tienes transacciones. `0` desactiva el recordatorio. |
| `keep_backups` | un número entero, 0 o más | `10` | Cuántas de las copias más recientes conservar en cada carpeta de copias: las que haces, las bases de datos que reemplazaron las restauraciones y las copias hechas antes de las actualizaciones. Cuando hay más, la interfaz ofrece eliminar las más antiguas (`ctrl+p`) y `thaler cli prune` las lista; no se elimina nada sin un sí. `0` lo conserva todo y deja de ofrecerlo. |

## Funciones

| Clave | Admite | Sin ella | Qué hace |
| --- | --- | --- | --- |
| `features.<name>` | `true` o `false` | desactivada | Activa una historia que se integró antes de estar terminada, para probarla antes. Un nombre que no existe se rechaza, y el error lista los que existen. En esta versión no hay nada tras un indicador. |

```toml
[features]
some_feature = true
```

## Reglas: `[[rule]]`

Cada `[[rule]]` da una categoría a las transacciones importadas. Se comprueban desde arriba, y gana la primera que coincide; una transacción con la que no coincide ninguna regla se queda en `Expenses:Uncategorized` o `Income:Uncategorized` para que decidas tú. Una regla se compara con el destinatario, el concepto y el texto de contabilización de una transacción, unidos por espacios, sin distinguir mayúsculas.

| Clave | Admite | Sin ella | Qué hace |
| --- | --- | --- | --- |
| `rule.contains` | texto | — | Coincide con una transacción cuyo texto contiene esto, sin distinguir mayúsculas ni tener en cuenta los espacios de alrededor. |
| `rule.regex` | una expresión regular ([sintaxis de Go](https://pkg.go.dev/regexp/syntax)) | — | Coincide con una transacción cuyo texto encuentra esta expresión, sin distinguir mayúsculas. |
| `rule.category` | un nombre de cuenta, como `Expenses:Groceries` | obligatoria | La categoría en la que se apunta una transacción que coincide. Una que aún no existe se crea al importar. `Equity:Transfers` marca el dinero movido entre tus propias cuentas; Thaler empareja solo las dos mitades de una transferencia así (mismo importe, dentro de tres días), y una regla solo marca una cuya otra mitad aún no se ha importado. |

Una regla tiene exactamente uno de `contains` y `regex`, y una `category`; una regla sin ninguno, con los dos, con una expresión rota o con una categoría que no es un nombre válido es un error que indica el número de la regla.

```toml
[[rule]]
contains = "REWE"
category = "Expenses:Groceries"

[[rule]]
regex = "b(ä|ae)ckerei|edeka|lidl|aldi"
category = "Expenses:Groceries"
```

## Perfiles de banco: `[[profile]]`

Un perfil dice cómo leer el extracto de un banco. Thaler trae tres: `sparkasse` (los extractos CSV-CAMT y CSV-MT940 de la banca en línea de la Sparkasse), `ofx` y `qif`. Un perfil tuyo solo hace falta para el CSV de otro banco, o para un archivo QIF cuyas fechas un perfil incluido lee al revés.

`thaler cli import --profile NAME` elige un perfil; sin esa opción, y siempre en la interfaz, se encuentra a partir del archivo (`auto`): un archivo OFX o QIF por cómo empieza, un archivo CSV por su cabecera. De los perfiles CSV cuyas columnas de fecha e importe tiene la cabecera, gana el que encuentra más de sus columnas, y entre iguales van primero los tuyos, en el orden en que están escritos. Un perfil tuyo con el nombre de uno incluido lo sustituye.

| Clave | Admite | Sin ella | Qué hace |
| --- | --- | --- | --- |
| `profile.name` | un nombre | obligatoria | Cómo lo llama `--profile`, sin distinguir mayúsculas, y con qué perfil dice una importación que leyó el archivo. |
| `profile.format` | `"csv"`, `"ofx"` o `"qif"` | `"csv"` | El tipo de archivo. Un archivo OFX no necesita nada más; un archivo QIF admite `date_formats` y `decimal_separator`; las columnas, el delimitador, la codificación y la regla de omisión son solo para CSV. |
| `profile.delimiter` | un carácter, o `"auto"` | `"auto"` | Lo que separa las columnas. `"auto"` prueba `;`, `,`, tabulador y `\|` y se queda con el que divide la cabecera en las columnas de fecha e importe. |
| `profile.encoding` | `"auto"`, `"utf-8"`, `"utf-16"`, `"utf-16le"`, `"utf-16be"` o `"windows-1252"` (también `"latin1"`, `"iso-8859-1"`) | `"auto"` | La codificación de caracteres. `"auto"` lee UTF-16 o UTF-8 cuando los bytes lo indican, y Windows-1252 en otro caso, que los bancos alemanes todavía usan. |
| `profile.decimal_separator` | `","` o `"."` | se deduce para cada importe | El separador decimal de los importes. Fíjalo cuando importes como `1.234` podrían leerse de las dos formas. |
| `profile.date_formats` | una lista de formatos hechos de `DD`, `MM`, `YY` y `YYYY`, como `"DD.MM.YYYY"` | `["YYYY-MM-DD", "DD.MM.YYYY"]` | Cómo se escriben las fechas. Cada fecha se lee con el primer formato que encaja; un archivo QIF usa el primer formato que lee todas sus fechas. También vale un formato de Go como `"2006-01-02"`. |
| `profile.skip_column` | un nombre de columna | no se omite ninguna fila | Con `skip_contains`: una fila cuyo valor en esta columna contiene `skip_contains`, sin distinguir mayúsculas, se deja fuera, como un movimiento pendiente. |
| `profile.skip_contains` | texto | no se omite ninguna fila | Véase `skip_column`. |

### Columnas: `[profile.columns]`

Cada clave lista los nombres de cabecera que pueden contener ese campo; se usa el primero que tenga el archivo. Los nombres de cabecera se comparan sin distinguir mayúsculas, y `ä`, `ö`, `ü` y `ß` cuentan como `ae`, `oe`, `ue` y `ss`. Un perfil CSV necesita `date` y `amount`; los demás quedan vacíos si no se dan.

| Clave | Admite | Sin ella | Qué hace |
| --- | --- | --- | --- |
| `profile.columns.date` | una lista de nombres de cabecera | obligatoria para CSV | La fecha de contabilización. |
| `profile.columns.amount` | una lista de nombres de cabecera | obligatoria para CSV | El importe, negativo para el dinero que sale. |
| `profile.columns.payee` | una lista de nombres de cabecera | el texto de contabilización | A quién se pagó o quién pagó. |
| `profile.columns.purpose` | una lista de nombres de cabecera | vacío | El concepto o la referencia. |
| `profile.columns.iban` | una lista de nombres de cabecera | vacío | La cuenta de la otra parte. |
| `profile.columns.booking_text` | una lista de nombres de cabecera | vacío | El tipo de movimiento (pago con tarjeta, transferencia, comisión). |
| `profile.columns.currency` | una lista de nombres de cabecera | la moneda de la cuenta | La moneda de cada fila. |

El destinatario, el concepto y el texto de contabilización son lo que comparan las reglas. Junto con la fecha, el importe y el IBAN forman también la clave que reconoce una fila importada antes, así que cambiar qué columnas lee un perfil puede hacer que un extracto se importe otra vez.

Un perfil tiene un problema cuando su formato, delimitador, codificación o separador decimal no es ninguno de los valores de arriba, o a un perfil CSV le falta la columna de fecha o de importe; el error nombra el perfil.

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
