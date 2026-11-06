package dev.kine.plugin.host;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** Minimal JSON parser and writer for the protocol. */
final class Json {
    private final String text;
    private int index;

    private Json(String text) {
        this.text = text;
    }

    static Object parse(String text) {
        Json parser = new Json(text);
        Object value = parser.parseValue();
        parser.skipWhitespace();
        if (parser.index != text.length()) {
            throw parser.error("unexpected data");
        }
        return value;
    }

    static String write(Object value) {
        StringBuilder builder = new StringBuilder();
        writeValue(builder, value);
        return builder.toString();
    }

    private Object parseValue() {
        skipWhitespace();
        if (index >= text.length()) {
            throw error("unexpected end");
        }
        char current = text.charAt(index);
        switch (current) {
            case '{':
                return parseObject();
            case '[':
                return parseArray();
            case '"':
                return parseString();
            case 't':
                expect("true");
                return Boolean.TRUE;
            case 'f':
                expect("false");
                return Boolean.FALSE;
            case 'n':
                expect("null");
                return null;
            default:
                if (current == '-' || (current >= '0' && current <= '9')) {
                    return parseNumber();
                }
                throw error("unexpected character '" + current + "'");
        }
    }

    private Map<String, Object> parseObject() {
        Map<String, Object> result = new LinkedHashMap<>();
        index++;
        skipWhitespace();
        if (peek('}')) {
            index++;
            return result;
        }
        while (true) {
            skipWhitespace();
            if (!peek('"')) {
                throw error("expected object key");
            }
            String key = parseString();
            skipWhitespace();
            expectChar(':');
            result.put(key, parseValue());
            skipWhitespace();
            if (peek('}')) {
                index++;
                return result;
            }
            expectChar(',');
        }
    }

    private List<Object> parseArray() {
        List<Object> result = new ArrayList<>();
        index++;
        skipWhitespace();
        if (peek(']')) {
            index++;
            return result;
        }
        while (true) {
            result.add(parseValue());
            skipWhitespace();
            if (peek(']')) {
                index++;
                return result;
            }
            expectChar(',');
        }
    }

    private String parseString() {
        expectChar('"');
        StringBuilder builder = new StringBuilder();
        while (index < text.length()) {
            char current = text.charAt(index++);
            if (current == '"') {
                return builder.toString();
            }
            if (current != '\\') {
                builder.append(current);
                continue;
            }
            if (index >= text.length()) {
                throw error("unfinished escape");
            }
            char escape = text.charAt(index++);
            switch (escape) {
                case '"':
                    builder.append('"');
                    break;
                case '\\':
                    builder.append('\\');
                    break;
                case '/':
                    builder.append('/');
                    break;
                case 'b':
                    builder.append('\b');
                    break;
                case 'f':
                    builder.append('\f');
                    break;
                case 'n':
                    builder.append('\n');
                    break;
                case 'r':
                    builder.append('\r');
                    break;
                case 't':
                    builder.append('\t');
                    break;
                case 'u':
                    if (index + 4 > text.length()) {
                        throw error("unfinished unicode escape");
                    }
                    builder.append((char) Integer.parseInt(text.substring(index, index + 4), 16));
                    index += 4;
                    break;
                default:
                    throw error("invalid escape");
            }
        }
        throw error("unterminated string");
    }

    private Object parseNumber() {
        int start = index;
        while (index < text.length()) {
            char current = text.charAt(index);
            if ((current >= '0' && current <= '9')
                    || current == '-'
                    || current == '+'
                    || current == '.'
                    || current == 'e'
                    || current == 'E') {
                index++;
            } else {
                break;
            }
        }
        String token = text.substring(start, index);
        if (token.indexOf('.') >= 0 || token.indexOf('e') >= 0 || token.indexOf('E') >= 0) {
            return Double.valueOf(token);
        }
        try {
            return Long.valueOf(token);
        } catch (NumberFormatException error) {
            return Double.valueOf(token);
        }
    }

    private void skipWhitespace() {
        while (index < text.length()) {
            char current = text.charAt(index);
            if (current == ' ' || current == '\t' || current == '\n' || current == '\r') {
                index++;
            } else {
                return;
            }
        }
    }

    private boolean peek(char wanted) {
        return index < text.length() && text.charAt(index) == wanted;
    }

    private void expectChar(char wanted) {
        if (index >= text.length() || text.charAt(index) != wanted) {
            throw error("expected '" + wanted + "'");
        }
        index++;
    }

    private void expect(String token) {
        if (!text.startsWith(token, index)) {
            throw error("expected " + token);
        }
        index += token.length();
    }

    private IllegalArgumentException error(String message) {
        return new IllegalArgumentException(message + " at offset " + index);
    }

    private static void writeValue(StringBuilder builder, Object value) {
        if (value == null) {
            builder.append("null");
            return;
        }
        if (value instanceof String string) {
            writeString(builder, string);
            return;
        }
        if (value instanceof Boolean || value instanceof Long || value instanceof Integer
                || value instanceof Double || value instanceof Float) {
            builder.append(value);
            return;
        }
        if (value instanceof Map<?, ?> map) {
            writeObject(builder, map);
            return;
        }
        if (value instanceof Iterable<?> items) {
            writeArray(builder, items);
            return;
        }
        throw new IllegalArgumentException("cannot encode " + value.getClass().getName());
    }

    private static void writeObject(StringBuilder builder, Map<?, ?> map) {
        builder.append('{');
        boolean first = true;
        for (Map.Entry<?, ?> entry : map.entrySet()) {
            if (!first) {
                builder.append(',');
            }
            first = false;
            writeString(builder, String.valueOf(entry.getKey()));
            builder.append(':');
            writeValue(builder, entry.getValue());
        }
        builder.append('}');
    }

    private static void writeArray(StringBuilder builder, Iterable<?> items) {
        builder.append('[');
        boolean first = true;
        for (Object item : items) {
            if (!first) {
                builder.append(',');
            }
            first = false;
            writeValue(builder, item);
        }
        builder.append(']');
    }

    private static void writeString(StringBuilder builder, String value) {
        builder.append('"');
        for (int i = 0; i < value.length(); i++) {
            char current = value.charAt(i);
            switch (current) {
                case '"':
                    builder.append("\\\"");
                    break;
                case '\\':
                    builder.append("\\\\");
                    break;
                case '\b':
                    builder.append("\\b");
                    break;
                case '\f':
                    builder.append("\\f");
                    break;
                case '\n':
                    builder.append("\\n");
                    break;
                case '\r':
                    builder.append("\\r");
                    break;
                case '\t':
                    builder.append("\\t");
                    break;
                default:
                    if (current < 0x20) {
                        builder.append(String.format("\\u%04x", (int) current));
                    } else {
                        builder.append(current);
                    }
            }
        }
        builder.append('"');
    }
}
