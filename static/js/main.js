function getQueryParams() {
  const queryString = window.location.search;
  const queryParams = new URLSearchParams(queryString);
  const params = {};

  queryParams.forEach((value, key) => {
    params[key] = value;
  });

  return params;
}

const queryParams = getQueryParams()
const error = document.querySelector(".error")
if (queryParams['error']) {
    error.style.display = "block"
    error.innerHTML = queryParams['error']
}
const info = document.querySelector(".info")
if (queryParams['info']) {
    info.style.display = "block"
    info.innerHTML = queryParams['info']
}