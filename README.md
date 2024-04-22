<a name="readme-top"></a>

[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![MIT License][license-shield]][license-url]



<!-- PROJECT LOGO -->
<br />
<div align="center">

  <h3 align="center">vMind Kubernetes Engine for OpenStack</h3>

  <p align="center">
     A middleware service that orchestrates the end-to-end deployment of Kubernetes clusters on OpenStack infrastructure, leveraging RKE2 to provide a fully managed and ready-to-use Kubernetes cluster solution.
    <br />
  </p>
</div>

## Getting Started

To get a local copy up and running, follow these simple steps.

### Prerequisites

- Go 1.21+
- OpenStack account and credentials

### Installation

1. Clone the repo
   ```sh
   git clone https://github.com/vmindtech/vke.git
   cd vke
   ```

2. Environment Configuration
    Create a configuration file (e.g., config-development.json or config-production.json) with the following structure:

   ```sh
        {
          "APP_NAME" : "vke",
          "PORT" : "80",
          "ENV" : "development",
          "VERSION" : "1.0.0",
          "MYSQL_URL": "USER:PASS@tcp(MYSQL_ADDRESS:3306)/DATABASE?charset=utf8&parseTime=true&loc=Europe%2FIstanbul",
          "COMPUTE_ENDPOINT": "https://OPENSTACK_DOMAIN:8774",
          "NETWORK_ENDPOINT": "https://OPENSTACK_DOMAIN:9696",
          "LOAD_BALANCER_ENDPOINT": "https://OPENSTACK_DOMAIN:9876",
          "IDENTITY_ENDPOINT": "https://OPENSTACK_DOMAIN:5000",
          "CLOUDFLARE_AUTH_TOKEN": "YOUR_CLOUDFLARE_TOKEN",
          "CLOUDFLARE_ZONE_ID": "YOUR_CLOUDFLARE_ZONE_ID",
          "CLOUDFLARE_DOMAIN": "YOUR_DOMAIN_FOR_DNS_RECORD",
          "PUBLIC_NETWORK_ID": "PUCLIC_NETWORK_UUID",
          "IMAGE_REF": "UBUNTU20.04-IMAGE-UUID",
          "NOVA_MICRO_VERSION": "2.88",
          "ENDPOINT":  "YOUR_VKE_API_PUBLIC_ADDRESS exp: http://vmind.com.tr/api/v1",
          "VKE_AGENT_VERSION": "1.0.0",
          "CLUSTER_AUTOSCALER_VERSION": "0.73",
          "CLOUD_PROVIDER_VKE_VERSION": "2.29.2"
        }
   ```
    Set the environment variable for your application's environment using the following commands in the terminal:

    ```sh
    export golang_env=development
    ```
    or 

    ```sh
    export golang_env=production
    ```
    These commands will help you specify the runtime environment for your application.
3. Create DATABASE

    ```
    mysql -h MYSQL_ADDRESS -u DATABSE_USER --password=YOUR_PASS --database=YOUR_DB < scripts/vke.sql 
    ```

4. Run the Application

    To run the application using Air for automatic reloading during development, use the following command in the terminal:

    ```sh
    air -c .air.toml
    ```

    This command will start the application and automatically reload it whenever code changes are detected, making your development process faster and more efficient.


## Running the Application with Docker

To run the application with Docker, follow these steps:

1. Build the Docker image or pull it from [Docker Hub](https://hub.docker.com/r/vmindtech/vke-application):
    - To build the Docker image locally:
        ```sh
        docker build -t vmindtech/vke-app .
        ```

    - Alternatively, you can pull the ready-made image from [Docker Hub](https://hub.docker.com/r/vmindtech/vke-application):
        ```sh
        docker pull vmindtech/vke-application:tag
        ```
        Replace `tag` with the desired version tag, for example `1.0`.


2. Run the Docker container:
    ```sh
    docker run -ti -v /opt/vke/config-production.json:/config-production.json -e golang_env='production' -p 8080:80 --name vke-application vmindtech/vke-application:tag
    ```
    Replace `tag` with the desired version tag, for example `1.0`.

    This command mounts either the `config-production.json` or `config-development.json` file from your host machine into the container, sets the `golang_env` environment variable to 'production' or 'development' accordingly, and forwards requests from port `8080` on your host machine to port `80` inside the Docker container. You can replace `/opt/vke/config-production.json` or `/opt/vke/config-development.json` with the path to your actual configuration file and use any port you prefer.

3. View the application in your browser:
    Navigate to `http://localhost:8080` in your browser to view the application.

4. Stopping and removing the container:
    ```sh
    docker stop vke-application
    docker rm vke-application
    ```

    Use the above commands to stop and remove the container when you're done.

Once you've successfully run the application with Docker, you can access it at `http://localhost:8080`.

<!-- LICENSE -->
## License

Distributed under the APACHE-2.0 License. See `LICENSE` for more information.


[contributors-shield]: https://img.shields.io/github/contributors/vmindtech/vke?style=for-the-badge
[contributors-url]: https://github.com/vmindtech/vke/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/vmindtech/vke?style=for-the-badge
[forks-url]: https://github.com/vmindtech/vke/network/members
[stars-shield]: https://img.shields.io/github/stars/vmindtech/vke?style=for-the-badge
[stars-url]: https://github.com/vmindtech/vke/stargazers
[issues-shield]: https://img.shields.io/github/issues/vmindtech/vke?style=for-the-badge
[issues-url]: https://github.com/vmindtech/vke/issues
[license-shield]: https://img.shields.io/github/license/vmindtech/vke?style=for-the-badge
[license-url]: https://github.com/vmindtech/vke/blob/main/LICENSE
