# opensearchtools
A cli tools to help manage Opensearch

## Install

### From release
You can download the opensearchtools from [release](https://github.com/disaster37/opensearchtools/releases)

### From docker registry
You can get from registry: `quay.io/webcenter/opensearchtools:<tag_name>` or `quay.io/webcenter/opensearchtools:2.x`

## Contribute

You PR are always welcome. Please use the righ branch to do PR:
 - 2.x for Opensearch 2.x
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
  - **separator**: The separator to concatain field when extract multi fields. Default to `|`
  - **split-file-field**: The field to use to split data into multi files. Default to `host.name`
  - **path**: The root path to create extracted files. Default to `.`

Sample of command:
```bash
opensearchtools_linux_amd64 --urls https://opensearch.company.com --user admin --password changeme --self-signed-certificate export-data --from now-12h --to now --date-field "@timestamp" --index "logs-*" --query "labels.application: app1 AND labels.environment: staging" --fields log.original --split-file-field host.name --path /tmp
```