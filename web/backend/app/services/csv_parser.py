import csv
import io


def parse_csv(text: str) -> list[dict]:
    """Parse imapsync batch CSV format.

    Expected format (semicolon separated):
    host1;user1;password1;host2;user2;password2;extra_options;

    Lines starting with # are comments. Empty lines are skipped.
    """
    rows = []
    reader = csv.reader(io.StringIO(text), delimiter=';')

    for line_num, fields in enumerate(reader, 1):
        # Skip empty lines and comments
        if not fields or (fields[0] and fields[0].strip().startswith('#')):
            continue

        # Strip whitespace
        fields = [f.strip() for f in fields]

        # Need at least 6 fields: host1, user1, pass1, host2, user2, pass2
        if len(fields) < 6:
            continue

        row = {
            'host1': fields[0],
            'user1': fields[1],
            'password1': fields[2],
            'host2': fields[3],
            'user2': fields[4],
            'password2': fields[5],
        }

        # Optional extra_options field (7th column)
        if len(fields) > 6 and fields[6]:
            row['extra_options'] = _parse_extra_options(fields[6])

        # Skip rows with empty required fields
        if all(row.get(k) for k in ['host1', 'user1', 'password1', 'host2', 'user2', 'password2']):
            rows.append(row)

    return rows


def _parse_extra_options(options_str: str) -> dict:
    """Parse extra options string into a dict.

    Format: "--option1 value1 --option2 --flag"
    """
    result = {}
    parts = options_str.split()
    i = 0
    while i < len(parts):
        if parts[i].startswith('--'):
            key = parts[i].lstrip('-')
            if i + 1 < len(parts) and not parts[i + 1].startswith('--'):
                result[key] = parts[i + 1]
                i += 2
            else:
                result[key] = True
                i += 1
        else:
            i += 1
    return result
