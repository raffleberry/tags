FROM ubuntu:24.04

WORKDIR /app

RUN apt update && apt --no-install-recommends install -y gcc g++ make cmake libutfcpp-dev zlib1g-dev 

COPY bake /app/

RUN chmod +x /app/bake

CMD [ "/app/bake", "taglib" ]