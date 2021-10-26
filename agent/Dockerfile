# docker build -t registry.cn-hangzhou.aliyuncs.com/dice/dop:1.3-jacoco-server-11 .
FROM zhmpidnm.mirror.aliyuncs.com/library/golang:1.16 as builder

COPY . /app/jacoco
WORKDIR /app/jacoco

# go build
RUN GOOS=linux GOARCH=amd64 go build -o /app/run /app/jacoco/cmd/main.go

FROM registry.erda.cloud/erda/terminus-openjdk:v11.0.6

RUN mkdir -p /usr/share/maven /usr/share/maven/ref \
  && curl -fsSL -o /tmp/apache-maven.tar.gz https://mirrors.bfsu.edu.cn/apache/maven/maven-3/3.6.3/binaries/apache-maven-3.6.3-bin.tar.gz \
  && tar -xzf /tmp/apache-maven.tar.gz -C /usr/share/maven --strip-components=1 \
  && rm -f /tmp/apache-maven.tar.gz \
  && ln -s /usr/share/maven/bin/mvn /usr/bin/mvn

COPY --from=builder /app/jacoco/files/extract-jar.sh /app/extract-jar.sh
COPY --from=builder /app/jacoco/files/glob.sh /app/glob.sh
COPY --from=builder /app/jacoco/files/jacococli.jar /app/jacococli.jar
COPY --from=builder /app/run /app/run

RUN mkdir -p /root/.m2


ENV MAVEN_HOME /usr/share/maven
ENV MAVEN_CONFIG "/root/.m2"

WORKDIR /app

CMD ["/app/run"]
