local utils = import 'mixin-utils/utils.libsonnet';
local filename = 'mimir-slow-queries.json';

(import 'dashboard-utils.libsonnet') {
  [filename]:
    ($.dashboard('Slow queries') + { uid: std.md5(filename) })
    .addClusterSelectorTemplates(false)
    .addRow(
      $.row('Accross tenants')
      .addPanel(
        $.panel('Response time') +
        $.lokiMetricsQueryPanel(
          [
            'quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap duration_seconds(response_time) [$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
            'quantile_over_time(0.5, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap duration_seconds(response_time) [$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
          ],
          ['p99', 'p50'],
          unit='s',
        )
      )
      .addPanel(
        $.panel('Fetched series') +
        $.lokiMetricsQueryPanel(
          [
            'quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap fetched_series_count[$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
            'quantile_over_time(0.5, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap fetched_series_count[$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
          ],
          ['p99', 'p50'],
        )
      )
      .addPanel(
        $.panel('Fetched chunks') +
        $.lokiMetricsQueryPanel(
          [
            'quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap fetched_chunk_bytes[$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
            'quantile_over_time(0.5, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap fetched_chunk_bytes[$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
          ],
          ['p99', 'p50'],
          unit='bytes',
        )
      )
      .addPanel(
        $.panel('Response size') +
        $.lokiMetricsQueryPanel(
          [
            'quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap response_size_bytes[$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
            'quantile_over_time(0.5, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap response_size_bytes[$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
          ],
          ['p99', 'p50'],
          unit='bytes',
        )
      )
      .addPanel(
        $.panel('Time span') +
        $.lokiMetricsQueryPanel(
          [
            'quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap duration_seconds(length) [$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
            'quantile_over_time(0.5, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap duration_seconds(length) [$__auto]) by ()' % [$._config.per_cluster_label, $._config.per_namespace_label],
          ],
          ['p99', 'p50'],
          unit='s',
        )
      )
    )
    .addRow(
      $.row('Top 10 tenants') { collapse: true }
      .addPanel(
        $.panel('P99 response time') +
        $.lokiMetricsQueryPanel(
          'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap duration_seconds(response_time) [$__auto]) by (user))' % [$._config.per_cluster_label, $._config.per_namespace_label],
          '{{user}}',
          unit='s',
        )
      )
      .addPanel(
        $.panel('P99 fetched series') +
        $.lokiMetricsQueryPanel(
          'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap fetched_series_count[$__auto]) by (user))' % [$._config.per_cluster_label, $._config.per_namespace_label],
          '{{user}}',
        )
      )
      .addPanel(
        $.panel('P99 fetched chunks') +
        $.lokiMetricsQueryPanel(
          'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap fetched_chunk_bytes[$__auto]) by (user))' % [$._config.per_cluster_label, $._config.per_namespace_label],
          '{{user}}',
          unit='bytes',
        )
      )
      .addPanel(
        $.panel('P99 response size') +
        $.lokiMetricsQueryPanel(
          'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap response_size_bytes[$__auto]) by (user))' % [$._config.per_cluster_label, $._config.per_namespace_label],
          '{{user}}',
          unit='bytes',
        )
      )
      .addPanel(
        $.panel('P99 time span') +
        $.lokiMetricsQueryPanel(
          'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap duration_seconds(length) [$__auto]) by (user))' % [$._config.per_cluster_label, $._config.per_namespace_label],
          '{{user}}',
          unit='s',
        )
      )
    )
    .addRow(
      (
        $.row('Top 10 User-Agents') { collapse: true }
        .addPanel(
          $.panel('P99 response time') +
          $.lokiMetricsQueryPanel(
            'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap duration_seconds(response_time) [$__auto]) by (user_agent))' % [$._config.per_cluster_label, $._config.per_namespace_label],
            '{{user_agent}}',
            unit='s',
          )
        )
        .addPanel(
          $.panel('P99 fetched series') +
          $.lokiMetricsQueryPanel(
            'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap fetched_series_count[$__auto]) by (user_agent))' % [$._config.per_cluster_label, $._config.per_namespace_label],
            '{{user_agent}}',
          )
        )
        .addPanel(
          $.panel('P99 fetched chunks') +
          $.lokiMetricsQueryPanel(
            'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap fetched_chunk_bytes[$__auto]) by (user_agent))' % [$._config.per_cluster_label, $._config.per_namespace_label],
            '{{user_agent}}',
            unit='bytes',
          )
        )
        .addPanel(
          $.panel('P99 response size') +
          $.lokiMetricsQueryPanel(
            'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap response_size_bytes[$__auto]) by (user_agent))' % [$._config.per_cluster_label, $._config.per_namespace_label],
            '{{user_agent}}',
            unit='bytes',
          )
        )
        .addPanel(
          $.panel('P99 time span') +
          $.lokiMetricsQueryPanel(
            'topk(10, quantile_over_time(0.99, {%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | unwrap duration_seconds(length) [$__auto]) by (user_agent))' % [$._config.per_cluster_label, $._config.per_namespace_label],
            '{{user_agent}}',
            unit='s',
          )
        )
      )
    )
    .addRow(
      $.row('')
      .addPanel(
        {
          height: '500px',
          title: 'Slow queries',
          type: 'table',
          datasource: '${loki_datasource}',

          // Query logs from Loki.
          targets: [
            {
              local extraFields = [
                'response_time_seconds="{{ duration .response_time }}"',
                'param_step_seconds="{{ div .param_step 1000 }}"',
                'length_seconds="{{ duration .length }}"',
              ],
              // Filter out the remote read endpoint.
              expr: '{%s=~"$cluster",%s=~"$namespace",name=~"query-frontend.*"} |= "query stats" != "/api/v1/read" | logfmt | user=~"${tenant_id}" | user_agent=~"${user_agent}" | response_time > ${min_duration} | label_format %s' % [$._config.per_cluster_label, $._config.per_namespace_label, std.join(',', extraFields)],
              instant: false,
              legendFormat: '',
              range: true,
              refId: 'A',
            },
          ],

          // Use Grafana transformations to display fields in a table.
          transformations: [
            {
              // Convert labels to fields.
              id: 'extractFields',
              options: {
                source: 'labels',
              },
            },
            {
              id: 'organize',
              options: {
                // Hide fields we don't care.
                local hiddenFields = ['caller', 'cluster', 'container', 'host', 'id', 'job', 'level', 'line', 'method', 'msg', 'name', 'namespace', 'path', 'pod', 'pod_template_hash', 'query_wall_time_seconds', 'stream', 'traceID', 'tsNs', 'labels', 'Line', 'Time', 'gossip_ring_member', 'component', 'response_time', 'param_step', 'length'],

                excludeByName: {
                  [field]: true
                  for field in hiddenFields
                },

                // Order fields.
                local orderedFields = ['ts', 'status', 'user', 'length_seconds', 'param_start', 'param_end', 'param_time', 'param_step_seconds', 'param_query', 'response_time_seconds', 'err'],

                indexByName: {
                  [orderedFields[i]]: i
                  for i in std.range(0, std.length(orderedFields) - 1)
                },

                // Rename fields.
                renameByName: {
                  ts: 'Completion date',
                  user: 'Tenant ID',
                  param_query: 'Query',
                  param_step_seconds: 'Step',
                  param_start: 'Start',
                  param_end: 'End',
                  param_time: 'Time (instant query)',
                  response_time_seconds: 'Duration',
                  length_seconds: 'Time span',
                  err: 'Error',
                },
              },
            },
            {
              // Transforma some fields into numbers so sorting in the table doesn't sort them lexicographically.
              id: 'convertFieldType',
              options: {
                local numericFields = ['estimated_series_count', 'fetched_chunk_bytes', 'fetched_chunks_count', 'fetched_index_bytes', 'fetched_series_count', 'queue_time_seconds', 'response_size_bytes', 'results_cache_hit_bytes', 'results_cache_miss_bytes', 'sharded_queries', 'split_queries', 'Time span', 'Duration', 'Step', 'queue_time_seconds'],

                conversions: [
                  {
                    targetField: fieldName,
                    destinationType: 'number',
                  }
                  for fieldName in numericFields
                ],
              },
            },
          ],

          fieldConfig: {
            // Configure overrides to nicely format field values.
            overrides:
              local bytesFields = ['fetched_chunk_bytes', 'fetched_index_bytes', 'response_size_bytes', 'results_cache_hit_bytes', 'results_cache_miss_bytes'];
              [
                {
                  matcher: { id: 'byName', options: fieldName },
                  properties: [{ id: 'unit', value: 'bytes' }],
                }
                for fieldName in bytesFields
              ] +
              local shortFields = ['estimated_series_count', 'fetched_chunks_count', 'fetched_series_count'];
              [
                {
                  matcher: { id: 'byName', options: fieldName },
                  properties: [{ id: 'unit', value: 'short' }],
                }
                for fieldName in shortFields
              ] +
              local secondsFields = ['Time span', 'Duration', 'Step', 'queue_time_seconds'];
              [
                {
                  matcher: { id: 'byName', options: fieldName },
                  properties: [{ id: 'unit', value: 's' }],
                }
                for fieldName in secondsFields
              ],
          },
        },
      )
    )
    + {
      templating+: {
        list+: [
          // Add the Loki datasource.
          {
            type: 'datasource',
            name: 'loki_datasource',
            label: 'Loki data source',
            query: 'loki',
            hide: 0,
            includeAll: false,
            multi: false,
          },
          // Add a variable to configure the min duration.
          {
            local defaultValue = '5s',

            type: 'textbox',
            name: 'min_duration',
            label: 'Min duration',
            hide: 0,
            options: [
              {
                selected: true,
                text: defaultValue,
                value: defaultValue,
              },
            ],
            current: {
              // Default value.
              selected: true,
              text: defaultValue,
              value: defaultValue,
            },
            query: defaultValue,
          },
          // Add a variable to configure the tenant to filter on.
          {
            local defaultValue = '.*',

            type: 'textbox',
            name: 'tenant_id',
            label: 'Tenant ID',
            hide: 0,
            options: [
              {
                selected: true,
                text: defaultValue,
                value: defaultValue,
              },
            ],
            current: {
              // Default value.
              selected: true,
              text: defaultValue,
              value: defaultValue,
            },
            query: defaultValue,
          },
          // Add a variable to configure the tenant to filter on.
          {
            local defaultValue = '.*',

            type: 'textbox',
            name: 'user_agent',
            label: 'User-Agent HTTP Header',
            hide: 0,
            options: [
              {
                selected: true,
                text: defaultValue,
                value: defaultValue,
              },
            ],
            current: {
              // Default value.
              selected: true,
              text: defaultValue,
              value: defaultValue,
            },
            query: defaultValue,
          },
        ],
      },
    } + {
      templating+: {
        list: [
          // Do not allow to include all namespaces otherwise this dashboard
          // risks to explode because it shows resources per pod.
          l + (if (l.name == 'namespace') then { includeAll: false } else {})
          for l in super.list
        ],
      },
    } + {
      // No auto-refresh by default.
      refresh: '',
    },
}
