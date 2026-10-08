# opensearchtools
A cli tools to help manage Opensearch

## Install

### From release
You can download the opensearchtools from [release](https://github.com/disaster37/opensearchtools/releases)

### From docker registry
You can get from registry: `quay.io/webcenter/opensearchtools:<tag_name>` or `quay.io/webcenter/opensearchtools:3.x`

## Contribute

You PR are always welcome. Please use the righ branch to do PR:
 - 3.x for Opensearch 3.x
Don't forget to add test if you add some functionalities.

To build, you can use the following command line:
```sh
make build
```

To lauch golang test, you can use the folowing command line:
```sh
make test
```

## CLI

### Global options

The following parameters are available for all commands line :
- **--urls**: The Opensearch URL. For exemple https://opensearch.company.com. Alternatively you can use environment variable `OPENSEARCH_URLS`. You can set multiple urls.
- **--user**: The login to connect on Opensearch. Alternatively you can use environment variable `OPENSEARCH_USER`.
- **--password**: The password to connect on Opensearch. Alternatively you can use environment variable `OPENSEARCH_PASSWORD`.
- **--self-signed-certificate**: Disable the check of server SSL certificate
- **--debug**: Enable the debug mode
- **--help**: Display help for the current command


You can set also this parameters on yaml file (one or all) and use the parameters `--config` with the path of your Yaml file.
```yaml
---
urls: https://opensearch.company.com
user: admin
password: changeme
```


### Disable shard allocation

It permit to disable shard allocation. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
opensearchtools_linux_amd64 --urls https://opensearch.company.com --user admin --password changeme --self-signed-certificate disable-routing-allocation
```

### Enable shard allocation

It permit to enable shard allocation. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
opensearchtools_linux_amd64 --urls https://opensearch.company.com --user admin --password changeme --self-signed-certificate enable-routing-allocation
```


### Check the number of nodes availables

It permit to check that the cluster have the number of available nodes

__parameters__:
  - **number-nodes** (required): number of nodes you expected

Sample of command:
```bash
opensearchtools_linux_amd64 --urls https://opensearch.company.com --user admin --password changeme --self-signed-certificate check-number-nodes --number-nodes 6
```

### Check if node is available in cluster

It permit to check if node is available on cluster

__parameters__:
  - **node-name** (required): The node name
  - **labels**: You can also search the `node-name` on labels instead of real node name. It usefull if you need use key that is not the real node name.

Sample of command:
```bash
opensearchtools_linux_amd64 --urls https://opensearch.company.com --user admin --password changeme --self-signed-certificate check-node-online --node-name es-master-01 --labels node_name
```

### Export data

It permit to rebuild log file from data stored on Opensearch. It usefull when use Opensearch as log storage.

__parameters__:
  - **from**: From time to export data. Default to `now-24h`
  - **to**: To time to export data. Default to `now`
  - **date-field**: The date field to range over. Default to `@timestamp`
  - **index**: The index to export data. Default to `_all_`
  - **query** (required): To query to export data. The query as Lucene query (string query format)
  - **fields**: Fields to extracts. Default to `log.original`
  - **filters**: Go regular expression(s) to keep only matching output lines before writing.
    Repeatable (`--filters "a" --filters "b"`); a line is kept if it matches ANY filter (OR
    semantics, like `grep -e`). The pattern is matched against the extracted fields joined by
    `--separator`. Note: because the CLI splits each `--filters` value on commas, avoid comma
    characters in a single pattern (e.g. `{2,3}` quantifiers); repeat the flag instead.
  - **separator**: The separator to concatain field when extract multi fields. Default to `|`
  - **split-file-field**: The field to use to split data into multi files. Default to `host.name`
  - **path**: The root path to create extracted files. Default to `.`
  - **open-index**: If you want to export data from index that is closed.

Sample of command:
```bash
opensearchtools_linux_amd64 --urls https://opensearch.company.com --user admin --password changeme --self-signed-certificate export-data --from now-12h --to now --date-field "@timestamp" --index "logs-*" --query "labels.application: app1 AND labels.environment: staging" --fields log.original --split-file-field host.name --path /tmp
```

Sample of command with filters:
```bash
opensearchtools_linux_amd64 --urls https://opensearch.company.com --user admin --password changeme --self-signed-certificate export-data --from now-12h --to now --index "logs-*" --query "*" --fields log.original --filters "ERROR" --filters "5[0-9][0-9]" --path /tmp
```

At Info level, the export logs the number of documents found on OpenSearch
(`Found N document to export`) and, after applying `--filters`, the number actually written
(`Exported N documents after filtering (from M found)`). With `--open-index`, the
`Exported ...` summary is a single aggregate across all processed indexes, and each index
also logs its own `Exported N documents after filtering (from M found) for index <name>` as
it completes. If the export is interrupted (Ctrl+C/SIGTERM), the accumulated counts are
logged as `Exported N documents after filtering (from M found) before interruption` and the
tool exits with code 1.

https://127.0.0.1:9200/.opensearchtools/_search
https://127.0.0.1:9200/.opensearchtools/_search


curl -k -u admin:vLPeJYa8.3RqtZCcAK6jNz  -H 'Content-Type: application/json' -XPOST https://127.0.0.1:9200/.opensearchtools/_search -d '{"query":{"term":{"indexes":".ds-tet-metadata-000001"}}}

curl -k -u admin:vLPeJYa8.3RqtZCcAK6jNz  -H 'Content-Type: application/json' -XPOST https://127.0.0.1:9200/.opensearchtools/_search -d '{"query":{"term":{"indexes":".ds-test-metadata-000001"}}}

## Minimum OpenSearch role

Below is the minimum custom OpenSearch role required to use the tooling. It grants the necessary cluster and index permissions for `OpenClosedIndex`, `ExportDataToFiles` (with and without `--open-index`), and metadata cleanup.

### Cluster permissions

| Action | Purpose |
|--------|---------|
| `cluster:admin/opendistro_security/auth/info` | Get current username for metadata tracking |

### Index permissions on `.opensearchtools`

| Action | Purpose |
|--------|---------|
| `indices:admin/create` | Create the `.opensearchtools` metadata index |
| `indices:admin/exists` | Check if `.opensearchtools` already exists |
| `indices:data/write/index` | Write/update metadata documents (session tracking, lock/unlock) |
| `indices:data/read/search` | Search metadata documents (cleanup, lock checks) |
| `indices:data/write/delete` | Delete metadata documents after cleanup |
| `indices:admin/get` | Read index settings |
| `indices:data/write/bulk*` | Bulk operations for metadata (cleanup) |
| `indices:data/write/update` | Update metadata documents (lock/unlock) |

### Index permissions on target data stream / indices

These apply to the data streams and backing indices you are exporting from or opening.

| Action | Purpose |
|--------|---------|
| `indices:admin/data_stream/get` | List backing indices of a data stream |
| `indices:admin/get` | Read index settings (creation date) |
| `indices:monitor/settings` | Check index state via cat indices API |
| `indices:admin/open` | Open a closed index (`--open-index` / `open-index` command) |
| `indices:admin/close` | Re-close an index after processing (cleanup) |
| `indices:data/read/search` | Search documents and manage PIT (export-data) |

### Minimal role definition (JSON)

```json
{
  "cluster_permissions": [
    "cluster:admin/opendistro_security/auth/info",
    "indices:data/write/bulk"
  ],
  "index_permissions": [
    {
      "index_patterns": [".opensearchtools"],
      "allowed_actions": [
        "indices:admin/create",
        "indices:admin/exists",
        "indices:data/write/index",
        "indices:data/write/delete",
        "indices:data/read/search",
        "indices:admin/get",
        "indices:data/write/bulk*",
        "indices:data/write/update"
      ]
    },
    {
      "index_patterns": ["logs-*", "<your-target-datastream-pattern>"],
      "allowed_actions": [
        "indices:admin/data_stream/get",
        "indices:admin/get",
        "indices:monitor/settings",
        "indices:admin/open",
        "indices:admin/close",
        "indices:admin/close*"
        "indices:data/read/search"
      ]
    }
  ]
}
```

Adjust the second `index_patterns` to match the actual data streams or indices you operate on.