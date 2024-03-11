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
- **--url**: The Opensearch URL. For exemple https://opensearch.company.com. Alternatively you can use environment variable `OPENSEARCH_URL`.
- **--user**: The login to connect on Opensearch. Alternatively you can use environment variable `OPENSEARCH_USER`.
- **--password**: The password to connect on Opensearch. Alternatively you can use environment variable `OPENSEARCH_PASSWORD`.
- **--self-signed-certificate**: Disable the check of server SSL certificate
- **--debug**: Enable the debug mode
- **--help**: Display help for the current command


You can set also this parameters on yaml file (one or all) and use the parameters `--config` with the path of your Yaml file.
```yaml
---
url: https://opensearch.company.com
user: admin
password: changeme
```


### Disable shard allocation

It permit to disable shard allocation. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate disable-routing-allocation
```

### Enable shard allocation

It permit to enable shard allocation. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate enable-routing-allocation
```

### Stop task for machine learning

It permit to temporarily stop the tasks associated with active machine leaning jobs and datafeeds. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate enable-ml-upgrade
```

### Start task for machine learning

It permit to start the tasks associated with active machine leaning jobs and datafeeds. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate disable-ml-upgrade
```

### Stop Watcher service

It permit to stop watcher service. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate stop-watcher-service
```

### Start Watcher service

It permit to start watcher service. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate start-watcher-service
```

### Stop ILM service

It permit to stop Index Lifecycle Management service. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate stop-ilm-service
```

### Start ILM service

It permit to start Index Lifecycle Management service. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate start-ilm-service
```

### Stop SLM service

It permit to stop Snapshot Lifecycle Management service. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate stop-slm-service
```

### Start SLM service

It permit to start Snapshot Lifecycle Management service. It usefull when reboot or upgrade nodes.

There are no parameter

Sample of command:
```bash
elktools_linux_amd64 --url https://elasticsearch.company.com --user elastic --password changeme --self-signed-certificate start-slm-service
```