---
title: "Publish Messages to a Topic"
date: 2019-03-26T09:44:15-07:00
weight: 1
---

Publishing a message to a topic with the Go CDK takes two steps:

1. Open the topic with the Pub/Sub provider of your choice (once per topic).
2. Send messages on the topic.

<!--more-->

The second step is the same across all providers because the first step
creates a value of the portable [`*pubsub.Topic`][] type. Publishing looks
like this:

{{< goexample src="gocloud.dev/pubsub.ExampleTopic_Send" imports="0" >}}

Note that the [semantics of message delivery][] can vary by provider.

The rest of this guide will discuss how to accomplish the first step: opening
a topic for your chosen Pub/Sub provider.

[`*pubsub.Topic`]: https://godoc.org/gocloud.dev/pubsub#Topic
[semantics of message delivery]: https://godoc.org/gocloud.dev/pubsub#hdr-At_most_once_and_At_least_once_Delivery

## Constructors versus URL openers

If you know that your program is always going to use a particular Pub/Sub
provider or you need fine-grained control over the connection settings, you
should call the constructor function in the driver package directly (like
`gcppubsub.OpenTopic`). However, if you want to change providers based on
configuration, you can use `pubsub.OpenTopic`, making sure you ["blank
import"][] the driver package to link it in. See the
[documentation on URLs][] for more details. This guide will show how to use
both forms for each pub/sub provider.

["blank import"]: https://golang.org/doc/effective_go.html#blank_import
[documentation on URLs]: {{< ref "/concepts/urls.md" >}}

## Amazon Simple Notification Service {#sns}

The Go CDK can publish to an Amazon [Simple Notification Service][SNS] (SNS)
topic. SNS URLs in the Go CDK use the Amazon Resource Name (ARN) to identify
the topic. You can specify the `region` query parameter to ensure your
application connects to the correct region, but otherwise `pubsub.OpenTopic`
will use the region found in the environment variables or your AWS CLI
configuration.

{{< goexample "gocloud.dev/pubsub/awssnssqs.Example_openSNSTopicFromURL" >}}

SNS messages are restricted to UTF-8 clean payloads. If your application
sends a message that contains non-UTF-8 bytes, then the Go CDK will
automatically [Base64][] encode the message and add a `base64encoded` message
attribute. When subscribing to messages on the topic through the Go CDK,
these will be [automatically Base64 decoded][SQS Subscribe], but if you are
receiving messages from a topic in a program that does not use the Go CDK,
you may need to manually Base64 decode the message payload.

[Base64]: https://en.wikipedia.org/wiki/Base64
[SQS Subscribe]: {{< relref "./subscribe.md#sqs" >}}
[SNS]: https://aws.amazon.com/sns/

### Amazon Simple Notification Service Constructor {#sns-ctor}

The [`awssnssqs.OpenSNSTopic`][] constructor opens an SNS topic. You must first
create an [AWS session][] with the same region as your topic:

{{< goexample "gocloud.dev/pubsub/awssnssqs.ExampleOpenSNSTopic" >}}

[`awssnssqs.OpenSNSTopic`]: https://godoc.org/gocloud.dev/pubsub/awssnssqs#OpenSNSTopic
[AWS session]: https://docs.aws.amazon.com/sdk-for-go/api/aws/session/

## Amazon Simple Notification Service {#sqs}

The Go CDK can publish to an Amazon [Simple Queue Service][SQS] (SQS)
topic. SQS URLs closely resemble the the queue URL, except the leading
`https://` is replaced with `awssqs://`. You can specify the `region`
query parameter to ensure your application connects to the correct region, but
otherwise `pubsub.OpenTopic` will use the region found in the environment
variables or your AWS CLI configuration.

{{< goexample "gocloud.dev/pubsub/awssnssqs.Example_openSQSTopicFromURL" >}}

SQS messages are restricted to UTF-8 clean payloads. If your application
sends a message that contains non-UTF-8 bytes, then the Go CDK will
automatically [Base64][] encode the message and add a `base64encoded` message
attribute. When subscribing to messages on the topic through the Go CDK,
these will be [automatically Base64 decoded][SQS Subscribe], but if you are
receiving messages from a topic in a program that does not use the Go CDK,
you may need to manually Base64 decode the message payload.

[Base64]: https://en.wikipedia.org/wiki/Base64
[SQS Subscribe]: {{< relref "./subscribe.md#sqs" >}}
[SQS]: https://aws.amazon.com/sqs/

### Amazon Simple Queue Service Constructor {#sqs-ctor}

The [`awssnssqs.OpenSQSTopic`][] constructor opens an SQS topic. You must first
create an [AWS session][] with the same region as your topic:

{{< goexample "gocloud.dev/pubsub/awssnssqs.ExampleOpenSQSTopic" >}}

[`awssnssqs.OpenSQSTopic`]: https://godoc.org/gocloud.dev/pubsub/awssnssqs#OpenSQSTopic
[AWS session]: https://docs.aws.amazon.com/sdk-for-go/api/aws/session/

## Google Cloud Pub/Sub {#gcp}

The Go CDK can publish to a Google [Cloud Pub/Sub][] topic. The URLs use the
project ID and the topic ID. `pubsub.OpenTopic` will use [Application Default
Credentials][GCP creds].

{{< goexample "gocloud.dev/pubsub/gcppubsub.Example_openTopicFromURL" >}}

[Cloud Pub/Sub]: https://cloud.google.com/pubsub/docs/
[GCP creds]: https://cloud.google.com/docs/authentication/production

### Google Cloud Pub/Sub Constructor {#gcp-ctor}

The [`gcppubsub.OpenTopic`][] constructor opens a Cloud Pub/Sub topic. You
must first obtain [GCP credentials][GCP creds] and then create a gRPC
connection to Cloud Pub/Sub. (This gRPC connection can be reused among
topics.)

{{< goexample "gocloud.dev/pubsub/gcppubsub.ExampleOpenTopic" >}}

[`gcppubsub.OpenTopic`]: https://godoc.org/gocloud.dev/pubsub/gcppubsub#OpenTopic

## Azure Service Bus {#azure}

The Go CDK can publish to an [Azure Service Bus][] topic over [AMQP 1.0][].
The URL for publishing is the topic name. `pubsub.OpenTopic` will use the
environment variable `SERVICEBUS_CONNECTION_STRING` to obtain the Service Bus
connection string. The connection string can be obtained
[from the Azure portal][Azure connection string].

{{< goexample "gocloud.dev/pubsub/azuresb.Example_openTopicFromURL" >}}

[AMQP 1.0]: https://www.amqp.org/
[Azure connection string]: https://docs.microsoft.com/en-us/azure/service-bus-messaging/service-bus-dotnet-how-to-use-topics-subscriptions#get-the-connection-string
[Azure Service Bus]: https://azure.microsoft.com/en-us/services/service-bus/

### Azure Service Bus Constructor {#azure-ctor}

The [`azuresb.OpenTopic`][] constructor opens an Azure Service Bus topic. You
must first connect to the topic using the [Azure Service Bus library][] and
then pass it to `azuresb.OpenTopic`. There are also helper functions in the
`azuresb` package to make this easier.

{{< goexample "gocloud.dev/pubsub/azuresb.ExampleOpenTopic" >}}

[`azuresb.OpenTopic`]: https://godoc.org/gocloud.dev/pubsub/azuresb#OpenTopic
[Azure Service Bus library]: https://github.com/Azure/azure-service-bus-go

## RabbitMQ {#rabbitmq}

The Go CDK can publish to an [AMQP 0.9.1][] fanout exchange, the dialect of
AMQP spoken by [RabbitMQ][]. A RabbitMQ URL only includes the exchange name.
The RabbitMQ's server is discovered from the `RABBIT_SERVER_URL` environment
variable (which is something like `amqp://guest:guest@localhost:5672/`).

{{< goexample "gocloud.dev/pubsub/rabbitpubsub.Example_openTopicFromURL" >}}

[AMQP 0.9.1]: https://www.rabbitmq.com/protocol.html
[RabbitMQ]: https://www.rabbitmq.com

### RabbitMQ Constructor {#rabbitmq-ctor}

The [`rabbitpubsub.OpenTopic`][] constructor opens a RabbitMQ exchange. You
must first create an [`*amqp.Connection`][] to your RabbitMQ instance.

{{< goexample "gocloud.dev/pubsub/rabbitpubsub.ExampleOpenTopic" >}}

[`*amqp.Connection`]: https://godoc.org/github.com/streadway/amqp#Connection
[`rabbitpubsub.OpenTopic`]: https://godoc.org/gocloud.dev/pubsub/rabbitpubsub#OpenTopic

## NATS {#nats}

The Go CDK can publish to a [NATS][] subject. A NATS URL only includes the
subject name. The NATS server is discovered from the `NATS_SERVER_URL`
environment variable (which is something like `nats://nats.example.com`).

{{< goexample "gocloud.dev/pubsub/natspubsub.Example_openTopicFromURL" >}}

Because NATS does not natively support metadata, messages sent to NATS will
be encoded with [gob][].

[gob]: https://golang.org/pkg/encoding/gob/
[NATS]: https://nats.io/

### NATS Constructor {#nats-ctor}

The [`natspubsub.OpenTopic`][] constructor opens a NATS subject as a topic. You
must first create an [`*nats.Conn`][] to your NATS instance.

{{< goexample "gocloud.dev/pubsub/natspubsub.ExampleOpenTopic" >}}

[`*nats.Conn`]: https://godoc.org/github.com/nats-io/go-nats#Conn
[`natspubsub.OpenTopic`]: https://godoc.org/gocloud.dev/pubsub/natspubsub#OpenTopic

## Kafka {#kafka}

The Go CDK can publish to a [Kafka][] cluster. A Kafka URL only includes the
topic name. The brokers in the Kafka cluster are discovered from the
`KAFKA_BROKERS` environment variable (which is a comma-delimited list of
hosts, something like `1.2.3.4:9092,5.6.7.8:9092`).

{{< goexample "gocloud.dev/pubsub/kafkapubsub.Example_openTopicFromURL" >}}

[Kafka]: https://kafka.apache.org/

### Kafka Constructor {#kafka-ctor}

The [`kafkapubsub.OpenTopic`][] constructor opens a Kafka topic to publish
messages to. Depending on your Kafka cluster configuration (see
`auto.create.topics.enable`), you may need to provision the topic beforehand.

In addition to the list of brokers, you'll need a [`*sarama.Config`][], which
exposes many knobs that can affect performance and semantics; review and set
them carefully. [`kafkapubsub.MinimalConfig`][] provides a minimal config to get
you started.

{{< goexample "gocloud.dev/pubsub/kafkapubsub.ExampleOpenTopic" >}}

[`*sarama.Config`]: https://godoc.org/github.com/Shopify/sarama#Config
[`kafkapubsub.OpenTopic`]: https://godoc.org/gocloud.dev/pubsub/kafkapubsub#OpenTopic
[`kafkapubsub.MinimalConfig`]: https://godoc.org/gocloud.dev/pubsub/kafkapubsub#MinimalConfig

## In-Memory {#mem}

The Go CDK includes an in-memory Pub/Sub provider useful for local testing.
The names in `mem://` URLs are a process-wide namespace, so subscriptions to
the same name will receive messages posted to that topic. This is detailed
more in the [subscription guide][subscribe-mem].

{{< goexample "gocloud.dev/pubsub/mempubsub.Example_openTopicFromURL" >}}

[subscribe-mem]: {{< ref "./subscribe.md#mem" >}}

### In-Memory Constructor {#mem-ctor}

To create an in-memory Pub/Sub topic, use the [`mempubsub.NewTopic`
function][]. You can use the returned topic to create in-memory
subscriptions, as detailed in the [subscription guide][subscribe-mem-ctor].

{{< goexample "gocloud.dev/pubsub/mempubsub.ExampleNewTopic" >}}

[`mempubsub.NewTopic` function]: https://godoc.org/gocloud.dev/pubsub/mempubsub#NewTopic
[subscribe-mem-ctor]: {{< ref "./subscribe.md#mem-ctor" >}}

