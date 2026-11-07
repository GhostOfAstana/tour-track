# Происхождение резервного снимка

Файлы `data/artists.json`, `data/locations.json`, `data/dates.json`, `data/relation.json` — исторические данные учебного Groupie Trackers API. Каждый раздел содержит 52 записи с ID 1–52. Это архив, а не текущая афиша. Концертные даты не переносились в будущее.

7 ноября 2026 года исходный `https://groupietrackers.herokuapp.com/api` и его четыре endpoint возвращали HTTP 404. Репозиторий первоначального API `https://github.com/01-edu/groupietrackers-api` также возвращал HTTP 404. Поэтому для работы при недоступности API сохранён публичный архив его JSON-ответов. Ни одна запись артиста, участника, локации или даты не придумана вручную.

## Побайтовый источник сохранённых файлов

Репозиторий: [GhostOfAstana/tour-track](https://github.com/GhostOfAstana/tour-track/tree/e5029bd6be53db8b0c1a96f3315dec6360a3e6e3/internal/catalog/testdata).

Зафиксированный commit: `e5029bd6be53db8b0c1a96f3315dec6360a3e6e3`.

| Файл | Постоянная ссылка | SHA-256 |
| --- | --- | --- |
| artists.json | [raw artists](https://raw.githubusercontent.com/GhostOfAstana/tour-track/e5029bd6be53db8b0c1a96f3315dec6360a3e6e3/internal/catalog/testdata/artists.json) | `5de19d94c7e6680601553c4965c67a7b0e5f36d6096ad2a202fe4f0721747108` |
| locations.json | [raw locations](https://raw.githubusercontent.com/GhostOfAstana/tour-track/e5029bd6be53db8b0c1a96f3315dec6360a3e6e3/internal/catalog/testdata/locations.json) | `055a09cbaff65994b38c41e0d559a48bc66f11b7cce3e4380942e5df2dad2d38` |
| dates.json | [raw dates](https://raw.githubusercontent.com/GhostOfAstana/tour-track/e5029bd6be53db8b0c1a96f3315dec6360a3e6e3/internal/catalog/testdata/dates.json) | `45c2a452e83bce704dcd6836a9c831eb902507cbc283b694486eb8cb97acbd1a` |
| relation.json | [raw relation](https://raw.githubusercontent.com/GhostOfAstana/tour-track/e5029bd6be53db8b0c1a96f3315dec6360a3e6e3/internal/catalog/testdata/relation.json) | `2476f36799a21c4b09c825e582274bd6d5b4cd839a928acc50943cd8d2d653db` |

Файлы скачаны без изменения байтов. Используются только общедоступные JSON-данные API; программный код перечисленных проектов не копировался.

## Независимая сверка

После разбора JSON и снятия внешней оболочки `index` содержимое всех четырёх разделов полностью совпало ещё с двумя публичными копиями:

- [itsliamine/groupie-tracker, каталог json](https://github.com/itsliamine/groupie-tracker/tree/31c0460f755f8e5a1bf514c296ff8c61b045ca33/json), commit `31c0460f755f8e5a1bf514c296ff8c61b045ca33`.
- [UgbabeOG/Groupie-tracker, каталог data](https://github.com/UgbabeOG/Groupie-tracker/tree/aa4b5384c8e89b3c324c5f928bf250c09f33c270/data), commit `aa4b5384c8e89b3c324c5f928bf250c09f33c270`.

Сверялись все поля и все значения, а не только число артистов. Различаются форматирование и наличие оболочки `index`; фактические записи совпадают.

Это подтверждает согласованность публичных архивов, но не доказывает неизменность данных относительно последнего ответа уже недоступного исходного сервера. Архив явно обозначается пользователю; успешная загрузка живого API заменяет его.

Сохранено исходное написание имён API, в том числе `John Daecon`, `Roger Meddows-Taylor` и `Doug Fogie`, поскольку аудит проверяет именно ответы API. Ссылки на оригинальные изображения в сырых JSON также оставлены без изменения.

Важное расхождение с текстом аудита: у Travis Scott архив содержит `sao_paulo-brazil`, а в ТЗ написано `sao_paulo-brasil`. Во всех трёх сверенных копиях значение одинаково: `brazil`. Оно сохранено без подмены. Классификатор считает `brasil` и `brazil` одной страной; эта орфографическая разница не влияет на число стран и баллы.

Время, возвращаемое `seed.Load`, — `2026-10-29T01:59:18Z`, дата коммита источника. Фактическая дата загрузки данных с первоначального API неизвестна; дата получения файлов из GitHub — 7 ноября 2026 года.

## Локальные фотографии

Все 52 исходные ссылки на фотографии принадлежат недоступному API. Чтобы браузер не отправлял по 52 заведомо неуспешных запроса при открытии каталога, `seed.Load` заменяет только поле `Image` в готовой модели на локальную фотографию Wikimedia Commons. Сырые JSON-файлы при этом не меняются. Это отдельно выбранные иллюстрации артистов, а не восстановленные оригинальные фотографии API.

Источники, авторы и свободные лицензии перечислены в [IMAGES.md](IMAGES.md) и машинном манифесте `images.json`. Миниатюры лежат в `images/` и занимают суммарно 4 482 101 байт. Программный код этих изображений не создаёт и не генерирует. В карточке артиста должна показываться атрибуция Commons; конечная резервная заглушка остаётся полезной при ошибке доставки изображения.
