FROM node:22-alpine
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY server.mjs runner.mjs import-policy.mjs ./
RUN mkdir /artifacts && chown node:node /artifacts
USER node
EXPOSE 8090
CMD ["node", "server.mjs"]
