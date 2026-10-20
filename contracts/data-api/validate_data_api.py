#!/usr/bin/env python3

# Позволяет использовать отложенное вычисление аннотаций типов.
# Полезно для совместимости аннотаций и уменьшения проблем
# с циклическими ссылками между типами.
from __future__ import annotations

# Модуль для обработки аргументов командной строки.
import argparse

# Используется для чтения JSON-файлов.
import json

# Используется для работы с регулярными выражениями.
import re

# Стандартный модуль sys.
# В текущей версии напрямую практически не используется,
# поэтому при необходимости импорт можно удалить.
import sys

# Удобный объектно-ориентированный интерфейс для работы
# с файлами и каталогами.
from pathlib import Path

# Универсальный тип для значений произвольного типа.
from typing import Any

# Библиотека для чтения YAML-файлов.
import yaml

# Draft202012Validator выполняет проверку документа
# по JSON Schema Draft 2020-12.
#
# FormatChecker дополнительно проверяет форматы,
# например URI, email и другие форматы,
# если они указаны в JSON Schema.
from jsonschema import Draft202012Validator, FormatChecker


# Регулярное выражение для поиска переменных вида
#
# ${token}
# ${user.id}
# ${access-token}
#
# Имя переменной должно начинаться с буквы или символа "_".
VARIABLE_RE = re.compile(
    r"\$\{([A-Za-z_][A-Za-z0-9_.-]*)\}"
)


# Регулярное выражение для поиска параметров
# непосредственно внутри URL-пути.
#
# Например
#
# /users/{userId}
#
# В данном случае будет обнаружен параметр userId.
PATH_PARAM_RE = re.compile(
    r"\{([A-Za-z_][A-Za-z0-9_.-]*)\}"
)


def load_structured(path: Path) -> Any:
    """
    Загружает структурированный документ из файла.

    Поддерживаются JSON и YAML.

    Для файлов с расширением .json используется json.loads().
    Для остальных файлов используется yaml.safe_load().
    """

    # Читаем содержимое файла в кодировке UTF-8.
    text = path.read_text(encoding="utf-8")

    # Если расширение файла JSON,
    # выполняем JSON-разбор документа.
    if path.suffix.lower() == ".json":
        return json.loads(text)

    # Для YAML и YAML-подобных файлов
    # используем безопасный YAML-парсер.
    return yaml.safe_load(text)


def format_path(parts: list[Any]) -> str:
    """
    Преобразует путь ошибки JSON Schema
    в понятный пользователю формат.

    Например

    ["checks", 0, "method"]

    преобразуется в

    $.checks[0].method
    """

    # Если путь пустой, ошибка относится
    # к корневому объекту документа.
    if not parts:
        return "$"

    # Начинаем путь от корневого объекта.
    out = "$"

    # Последовательно добавляем элементы пути.
    for part in parts:

        # Числовое значение означает индекс массива.
        if isinstance(part, int):
            out += f"[{part}]"

        # Остальные значения рассматриваются
        # как имена свойств объекта.
        else:
            out += f".{part}"

    return out


def collect_strings(value: Any):
    """
    Рекурсивно извлекает все строковые значения
    из словарей и списков.

    Функция используется далее для поиска
    конструкций ${variable} внутри request.
    """

    # Если получили обычную строку,
    # возвращаем её генератором.
    if isinstance(value, str):
        yield value

    # Если получили словарь,
    # рекурсивно проверяем все его значения.
    elif isinstance(value, dict):
        for item in value.values():
            yield from collect_strings(item)

    # Если получили список,
    # рекурсивно проверяем каждый его элемент.
    elif isinstance(value, list):
        for item in value:
            yield from collect_strings(item)


def semantic_checks(
    doc: dict[str, Any]
) -> tuple[list[str], list[str]]:
    """
    Выполняет дополнительные семантические проверки DATA-API.

    JSON Schema проверяет прежде всего структуру документа,
    типы полей и обязательные свойства.

    Эта функция проверяет связи между объектами,
    которые одной JSON Schema проверять неудобно.

    Возвращает два списка

    errors
        Критические ошибки.

    warnings
        Предупреждения, которые требуют внимания,
        но не обязательно делают документ недействительным.
    """

    # Список критических ошибок.
    errors: list[str] = []

    # Список предупреждений.
    warnings: list[str] = []

    # Получаем основную последовательность проверок.
    # Если checks отсутствует или равен null,
    # используем пустой список.
    checks = doc.get("checks") or []

    # Аналогично получаем шаги очистки.
    cleanup = doc.get("cleanup") or []

    # Собираем идентификаторы всех проверок.
    ids = [
        c.get("id")
        for c in checks
        if isinstance(c, dict)
    ]

    # Выявляем повторяющиеся идентификаторы.
    #
    # set используется для того, чтобы каждый
    # повторяющийся идентификатор выводился только один раз.
    duplicates = sorted({
        x
        for x in ids
        if x and ids.count(x) > 1
    })

    if duplicates:
        errors.append(
            "Повторяющиеся значения контрольных идентификаторов: "
            + ", ".join(duplicates)
        )

    # Формируем множество известных идентификаторов.
    #
    # Оно понадобится для проверки dependsOn.
    id_set = set(x for x in ids if x)

    # Здесь будут храниться переменные,
    # которые были извлечены предыдущими проверками.
    #
    # Например
    #
    # extract:
    #   token: $.access_token
    #
    # После этого token считается доступной переменной.
    produced_variables: set[str] = set()

    # Последовательно анализируем каждую проверку.
    for index, check in enumerate(checks):

        # Элемент checks должен быть объектом.
        # Структурную ошибку обычно поймает JSON Schema,
        # поэтому здесь просто пропускаем некорректный элемент.
        if not isinstance(check, dict):
            continue

        # Получаем идентификатор проверки.
        #
        # Если id отсутствует, для диагностических сообщений
        # используется временное значение вида #0.
        cid = check.get("id", f"#{index}")

        # ---------------------------------------------------------
        # Проверка зависимостей dependsOn
        # ---------------------------------------------------------

        for dep in check.get("dependsOn") or []:

            # Проверка не должна зависеть от неизвестного id.
            if dep not in id_set:
                errors.append(
                    f"Проверка {cid} зависит от неизвестного "
                    f"идентификатора проверки {dep}"
                )

            # Проверка не должна зависеть сама от себя.
            if dep == cid:
                errors.append(
                    f"Проверка {cid} не может зависеть сама от себя"
                )

        # ---------------------------------------------------------
        # Проверка path-параметров
        # ---------------------------------------------------------

        # Получаем HTTP-путь.
        #
        # Например
        #
        # /users/{userId}
        path = check.get("path", "")

        # Получаем фактически переданные path-параметры
        # из request.path.
        path_params = (
            (check.get("request") or {}).get("path") or {}
        )

        # Находим параметры непосредственно
        # в шаблоне URL.
        placeholders = set(PATH_PARAM_RE.findall(path))

        # Получаем имена параметров,
        # которые переданы в request.path.
        supplied = set(path_params.keys())

        # Параметры присутствуют в URL,
        # но отсутствуют в request.path.
        missing = placeholders - supplied

        # Параметры переданы в request.path,
        # но в самом URL их нет.
        extra = supplied - placeholders

        if missing:
            errors.append(
                f"Проверка {cid} не содержит значения "
                f"path-параметров для: "
                f"{', '.join(sorted(missing))}"
            )

        if extra:
            warnings.append(
                f"Проверка {cid} содержит неиспользуемые "
                f"path-параметры: "
                f"{', '.join(sorted(extra))}"
            )

        # ---------------------------------------------------------
        # Проверка переменных ${variable}
        # ---------------------------------------------------------

        used_variables: set[str] = set()

        # Рекурсивно извлекаем строки
        # из request.
        for text in collect_strings(
            check.get("request") or {}
        ):
            # Находим внутри строки все конструкции
            # вида ${variable}.
            used_variables.update(
                VARIABLE_RE.findall(text)
            )

        # Вычисляем переменные,
        # которые используются сейчас,
        # но ещё не были получены через extract.
        unknown = used_variables - produced_variables

        if unknown:
            warnings.append(
                f"Проверка {cid} использует переменные, "
                f"не извлечённые предыдущими проверками: "
                f"{', '.join(sorted(unknown))}. "
                "Требуется дополнительная проверка экспертом."
            )

        # ---------------------------------------------------------
        # Проверка extract
        # ---------------------------------------------------------

        for variable, expression in (
            check.get("extract") or {}
        ).items():

            # Выражение извлечения должно выглядеть
            # как JSONPath и начинаться с символа $.
            if not expression.startswith("$"):
                errors.append(
                    f"Проверка {cid} извлекает переменную "
                    f"{variable}, однако выражение должно "
                    f"быть JSONPath и начинаться с $"
                )

            # После текущей проверки переменная
            # становится доступной следующим проверкам.
            produced_variables.add(variable)

        # ---------------------------------------------------------
        # Проверка repeatable
        # ---------------------------------------------------------

        # Операции, изменяющие состояние сервера,
        # должны явно сообщать,
        # допускается ли их повторное выполнение.
        if (
            check.get("method")
            in {"POST", "PUT", "PATCH", "DELETE"}
            and "repeatable" not in check
        ):
            warnings.append(
                f"Проверка {cid} должна явно устанавливать "
                f"поле repeatable в true или false"
            )

        # ---------------------------------------------------------
        # Проверка Authorization
        # ---------------------------------------------------------

        # Получаем HTTP-заголовки запроса.
        headers = (
            (check.get("request") or {}).get("headers")
            or {}
        )

        # Поддерживаем оба наиболее вероятных
        # варианта написания заголовка.
        auth_value = (
            headers.get("Authorization")
            or headers.get("authorization")
        )

        # Если Authorization является обычной строкой
        # и внутри отсутствует ссылка на переменную,
        # существует вероятность,
        # что в DATA-API.yaml записан настоящий токен.
        if (
            isinstance(auth_value, str)
            and "${" not in auth_value
        ):
            warnings.append(
                f"Проверка {cid} содержит буквальное значение "
                f"заголовка Authorization. "
                "Не храните реальные учётные данные "
                "в DATA-API.yaml."
            )

    # -------------------------------------------------------------
    # Проверка cleanup
    # -------------------------------------------------------------

    for index, step in enumerate(cleanup):

        if not isinstance(step, dict):
            continue

        # Получаем id операции очистки.
        sid = step.get(
            "id",
            f"cleanup#{index}"
        )

        # Получаем путь cleanup-запроса.
        path = step.get("path", "")

        # Получаем значения path-параметров.
        path_params = (
            (step.get("request") or {}).get("path")
            or {}
        )

        # Извлекаем параметры из URL.
        placeholders = set(
            PATH_PARAM_RE.findall(path)
        )

        # Получаем предоставленные параметры.
        supplied = set(path_params.keys())

        # Определяем отсутствующие параметры.
        missing = placeholders - supplied

        if missing:
            errors.append(
                f"При очистке {sid} отсутствуют значения "
                f"path-параметров для: "
                f"{', '.join(sorted(missing))}"
            )

    # Возвращаем оба набора диагностических сообщений.
    return errors, warnings


def cross_check_openapi(
    doc: dict[str, Any],
    data_api_path: Path,
    explicit_openapi: str | None
) -> tuple[list[str], list[str]]:
    """
    Сопоставляет DATA-API.yaml с OpenAPI-документом.

    Проверяется наличие используемых DATA-API путей
    и HTTP-методов в OpenAPI.

    explicit_openapi позволяет передать OpenAPI
    через аргумент командной строки --openapi.

    Если аргумент не передан, путь берётся
    из api.openapi внутри DATA-API.
    """

    errors: list[str] = []
    warnings: list[str] = []

    # Приоритет имеет значение,
    # явно переданное через --openapi.
    #
    # Если его нет, используется api.openapi
    # из DATA-API.yaml.
    openapi_ref = (
        explicit_openapi
        or ((doc.get("api") or {}).get("openapi"))
    )

    # Отсутствие OpenAPI не делает DATA-API
    # автоматически недействительным.
    #
    # Поэтому возвращаем предупреждение.
    if not openapi_ref:
        return errors, [
            "Перекрёстная проверка OpenAPI пропущена, "
            "поскольку файл OpenAPI не был указан"
        ]

    # Формируем объект Path.
    openapi_path = Path(openapi_ref)

    # Если указан относительный путь,
    # считаем его относительно DATA-API.yaml.
    if not openapi_path.is_absolute():
        openapi_path = (
            data_api_path.parent / openapi_path
        ).resolve()

    # Проверяем наличие файла.
    if not openapi_path.exists():
        return [
            f"Файл OpenAPI не найден: {openapi_path}"
        ], warnings

    # Пытаемся прочитать OpenAPI.
    try:
        spec = load_structured(openapi_path)

    except Exception as exc:
        return [
            f"Не удаётся прочитать файл OpenAPI "
            f"{openapi_path}: {exc}"
        ], warnings

    # Корневой элемент OpenAPI должен быть объектом.
    if not isinstance(spec, dict):
        return [
            "Документ OpenAPI должен быть объектом"
        ], warnings

    # -------------------------------------------------------------
    # Проверка версии OpenAPI
    # -------------------------------------------------------------

    version = str(
        spec.get("openapi", "")
    )

    # Поддерживаются OpenAPI 3.0.x и 3.1.x.
    if not (
        version.startswith("3.0.")
        or version.startswith("3.1.")
    ):
        errors.append(
            f"Версия OpenAPI должна быть 3.0.x "
            f"или 3.1.x, обнаружено "
            f"{version or '<missing>'}"
        )

    # Получаем таблицу API-путей.
    paths = spec.get("paths") or {}

    # -------------------------------------------------------------
    # Проверка основных checks
    # -------------------------------------------------------------

    for check in (doc.get("checks") or []):

        # Получаем путь проверки.
        path = check.get("path")

        # HTTP-методы OpenAPI хранятся
        # в нижнем регистре.
        method = str(
            check.get("method", "")
        ).lower()

        # Сначала проверяем,
        # существует ли сам URL-путь.
        if path not in paths:
            errors.append(
                f"OpenAPI не содержит пути {path}, "
                f"используемого при проверке "
                f"{check.get('id')}"
            )
            continue

        # Затем проверяем наличие HTTP-метода
        # внутри найденного пути.
        if method not in (paths.get(path) or {}):
            errors.append(
                f"OpenAPI не содержит "
                f"{method.upper()} {path}, "
                f"используемого при проверке "
                f"{check.get('id')}"
            )

    # -------------------------------------------------------------
    # Проверка cleanup
    # -------------------------------------------------------------

    # Для cleanup несовпадения считаются предупреждениями,
    # а не критическими ошибками.
    for step in (doc.get("cleanup") or []):

        path = step.get("path")

        method = str(
            step.get("method", "")
        ).lower()

        if path not in paths:
            warnings.append(
                f"OpenAPI не содержит пути очистки {path}"
            )
            continue

        if method not in (paths.get(path) or {}):
            warnings.append(
                f"OpenAPI не содержит метода очистки "
                f"{method.upper()} {path}"
            )

    return errors, warnings


def main() -> int:
    """
    Основная функция CLI-валидатора.

    Последовательность работы

    1. Чтение аргументов командной строки.
    2. Загрузка DATA-API.yaml.
    3. Загрузка DATA-API.schema.json.
    4. Проверка JSON Schema.
    5. Выполнение семантических проверок.
    6. Сопоставление с OpenAPI.
    7. Вывод ошибок и предупреждений.
    8. Возврат соответствующего exit code.
    """

    # Создаём CLI-парсер.
    parser = argparse.ArgumentParser(
        description=(
            "Проверка DATA-API.yaml "
            "на соответствие DATA-API 1.0"
        )
    )

    # Основной позиционный аргумент.
    parser.add_argument(
        "file",
        help="Путь к файлу DATA-API.yaml"
    )

    # Необязательный путь к JSON Schema.
    parser.add_argument(
        "--schema",
        default=None,
        help="Путь к DATA-API.schema.json"
    )

    # Необязательный путь к OpenAPI.
    parser.add_argument(
        "--openapi",
        default=None,
        help=(
            "Необязательный явный путь "
            "к openapi.yaml или openapi.json"
        )
    )

    # Читаем параметры командной строки.
    args = parser.parse_args()

    # Преобразуем путь DATA-API
    # в абсолютный путь.
    data_api_path = Path(
        args.file
    ).resolve()

    # Если схема передана явно,
    # используем её.
    #
    # Иначе DATA-API.schema.json ищется
    # рядом с текущим Python-скриптом.
    schema_path = (
        Path(args.schema).resolve()
        if args.schema
        else Path(__file__).with_name(
            "DATA-API.schema.json"
        )
    )

    # -------------------------------------------------------------
    # Проверка существования входных файлов
    # -------------------------------------------------------------

    if not data_api_path.exists():
        print(
            f"ОШИБКА файл DATA-API не найден: "
            f"{data_api_path}"
        )
        return 2

    if not schema_path.exists():
        print(
            f"ОШИБКА схема данных не найдена: "
            f"{schema_path}"
        )
        return 2

    # -------------------------------------------------------------
    # Чтение DATA-API
    # -------------------------------------------------------------

    try:
        doc = load_structured(
            data_api_path
        )

    except Exception as exc:
        print(
            f"ОШИБКА невозможно прочитать файл "
            f"DATA-API {data_api_path.name}: {exc}"
        )
        return 2

    # -------------------------------------------------------------
    # Чтение JSON Schema
    # -------------------------------------------------------------

    try:
        schema = load_structured(
            schema_path
        )

    except Exception as exc:
        print(
            f"ОШИБКА не удаётся проанализировать "
            f"схему: {exc}"
        )
        return 2

    # Корневой DATA-API должен быть объектом.
    if not isinstance(doc, dict):
        print(
            "ОШИБКА файл DATA-API "
            "должен быть YAML-объектом"
        )
        return 1

    # -------------------------------------------------------------
    # JSON Schema validation
    # -------------------------------------------------------------

    validator = Draft202012Validator(
        schema,
        format_checker=FormatChecker()
    )

    # Получаем все ошибки JSON Schema.
    #
    # Они сортируются по пути,
    # чтобы вывод был стабильным и удобным.
    schema_errors = sorted(
        validator.iter_errors(doc),
        key=lambda e: list(e.absolute_path)
    )

    errors: list[str] = []
    warnings: list[str] = []

    # Форматируем сообщения JSON Schema.
    for error in schema_errors:
        errors.append(
            f"{format_path(list(error.absolute_path))}: "
            f"{error.message}"
        )

    # -------------------------------------------------------------
    # Семантические проверки
    # -------------------------------------------------------------

    semantic_errors, semantic_warnings = (
        semantic_checks(doc)
    )

    errors.extend(
        semantic_errors
    )

    warnings.extend(
        semantic_warnings
    )

    # -------------------------------------------------------------
    # Проверка соответствия OpenAPI
    # -------------------------------------------------------------

    openapi_errors, openapi_warnings = (
        cross_check_openapi(
            doc,
            data_api_path,
            args.openapi
        )
    )

    errors.extend(
        openapi_errors
    )

    warnings.extend(
        openapi_warnings
    )

    # -------------------------------------------------------------
    # Вывод предупреждений
    # -------------------------------------------------------------

    if warnings:
        print("ВНИМАНИЕ")

        for warning in warnings:
            print(
                f"  - {warning}"
            )

    # -------------------------------------------------------------
    # Вывод ошибок
    # -------------------------------------------------------------

    if errors:
        print("НЕДЕЙСТВИТЕЛЬНЫЙ")

        for error in errors:
            print(
                f"  - {error}"
            )

        # Код 1 означает,
        # что файл был прочитан,
        # однако проверку не прошёл.
        return 1

    # Если критических ошибок нет,
    # документ считается прошедшим проверку.
    print(
        "ПОДТВЕРЖДЕНИЕ файл DATA-API.yaml "
        "соответствует версии схемы 1.0"
    )

    # Код 0 означает успешное завершение.
    return 0


# Стандартная точка входа Python-программы.
#
# main() возвращает числовой код завершения,
# который передаётся операционной системе
# через SystemExit.
if __name__ == "__main__":
    raise SystemExit(main())