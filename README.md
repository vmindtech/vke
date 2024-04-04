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
          "VERSION" : "0.1.0",
          "MYSQL_URL": "",
          "COMPUTE_ENDPOINT": "",
          "NETWORK_ENDPOINT": "",
          "LOAD_BALANCER_ENDPOINT": "",
          "IDENTITY_ENDPOINT": "",
          "CLOUDFLARE_AUTH_TOKEN": "",
          "CLOUDFLARE_ZONE_ID": "",
          "CLOUDFLARE_DOMAIN": "",
          "PUBLIC_NETWORK_ID": "",
          "IMAGE_REF": "",
          "NOVA_MICRO_VERSION": "2.88",
          "ENDPOINT":  "",
          "VKE_AGENT_VERSION": "",
          "CLUSTER_AUTOSCALER_VERSION": "",
          "CLOUD_PROVIDER_VKE_VERSION": ""
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

3. Run the Application

    To run the application using Air for automatic reloading during development, use the following command in the terminal:

    ```sh
    air -c .air.toml
    ```

    This command will start the application and automatically reload it whenever code changes are detected, making your development process faster and more efficient.

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
