# JSON Output Structure

Run the prototype with:

```bash
go run . -format json
```

The JSON output is intentionally close to the console report while preserving
section order. It is a transport-friendly report shape, not the final RootPlane
API model.

## Top-Level Object

```json
{
  "title": "System Information",
  "generated_at": "2026-06-28T14:31:00-04:00",
  "rate_sample_interval_seconds": 1,
  "sections": []
}
```

- `title`: Report title.
- `generated_at`: RFC3339 timestamp when the report was generated.
- `rate_sample_interval_seconds`: Number of seconds used to calculate rate-based
  values such as CPU utilization, disk throughput, and network throughput.
- `sections`: Ordered report sections.

## Section

```json
{
  "name": "System",
  "fields": [
    { "label": "OS", "value": "Fedora Linux ..." }
  ],
  "groups": [],
  "lists": []
}
```

- `name`: Section name from the console output.
- `fields`: Ordered label/value pairs.
- `groups`: Repeated child objects such as disk volumes or network adapters.
- `lists`: Ordered lists such as open ports, installed packages, sessions, or
  problem services.

## Group

```json
{
  "name": "/",
  "fields": [
    { "label": "Device", "value": "/dev/nvme1n1p3" }
  ],
  "lists": []
}
```

Groups are used for repeated inventory records. Current examples include disk
volumes and network adapters.

## List

```json
{
  "label": "Packages",
  "values": ["package | version | publisher"],
  "displayed": 50,
  "total": 3001,
  "omitted_count": 2951
}
```

- `values`: String list currently matching the console-friendly text.
- `displayed`: Number of values included in this output.
- `total`: Total number of values found by the collector.
- `omitted_count`: Values not included because the console-oriented collector
  applied a display limit.

## Notes For API Design

- Treat this as a prototype report format. The server API should eventually use
  typed fields for values such as bytes, percentages, booleans, timestamps, and
  package identities.
- `N/A` means the value was unavailable, unsupported, blocked by permissions, or
  not detected by the current platform collector. The API should eventually
  distinguish those states.
- The JSON currently preserves console labels to keep the prototype easy to
  inspect. A production API should use stable snake_case field names.
